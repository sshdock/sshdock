#!/usr/bin/env bash
set -euo pipefail

die() {
	printf 'bootstrap: %s\n' "$*" >&2
	exit 1
}

need_env() {
	local name="$1"
	local value="${!name:-}"
	if [ -z "$value" ]; then
		die "$name is required"
	fi
}

need_command() {
	local name="$1"
	command -v "$name" >/dev/null 2>&1 || die "$name is required"
}

prefix_path() {
	local path="$1"
	if [ "$BOOTSTRAP_ROOT" = "/" ]; then
		printf '%s\n' "$path"
	else
		printf '%s/%s\n' "${BOOTSTRAP_ROOT%/}" "${path#/}"
	fi
}

run() {
	printf '+ %s\n' "$*" >&2
	"$@"
}

check_dependencies() {
	need_command docker
	need_command caddy
	need_command systemctl

	run docker version >/dev/null
	run docker compose version >/dev/null
	run caddy version >/dev/null
	run systemctl --version >/dev/null
}

ensure_root_for_real_install() {
	if [ "$BOOTSTRAP_ROOT" != "/" ]; then
		return
	fi
	if [ "$(id -u)" -ne 0 ]; then
		die "run with sudo or set RHUMBASE_BOOTSTRAP_ROOT for a harness install"
	fi
}

ensure_daemon_user() {
	if [ "$SKIP_USER" = "1" ]; then
		return
	fi
	if id -u "$DAEMON_USER" >/dev/null 2>&1; then
		return
	fi
	if [ "$(id -u)" -ne 0 ]; then
		die "daemon user $DAEMON_USER is missing; run as root to create it"
	fi
	need_command useradd
	run useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin "$DAEMON_USER"
}

maybe_chown() {
	local path="$1"
	if [ "$SKIP_CHOWN" = "1" ]; then
		return
	fi
	if [ "$(id -u)" -ne 0 ]; then
		die "cannot set ownership for $path without root"
	fi
	run chown -R "$DAEMON_USER:$DAEMON_USER" "$path"
}

prepare_directories() {
	local bin_dir_actual data_dir_actual apps_dir_actual systemd_dir_actual caddy_dir_actual
	bin_dir_actual="$(prefix_path "$INSTALL_BIN_DIR")"
	data_dir_actual="$(prefix_path "$DATA_DIR")"
	apps_dir_actual="$(prefix_path "$APPS_DIR")"
	systemd_dir_actual="$(prefix_path "$SYSTEMD_DIR")"
	caddy_dir_actual="$(dirname "$(prefix_path "$CADDY_CONFIG_PATH")")"

	run mkdir -p "$bin_dir_actual" "$data_dir_actual" "$apps_dir_actual" "$systemd_dir_actual" "$caddy_dir_actual"
	run chmod 0755 "$data_dir_actual" "$apps_dir_actual"
	maybe_chown "$data_dir_actual"
}

detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64)
			printf 'amd64\n'
			;;
		aarch64|arm64)
			printf 'arm64\n'
			;;
		*)
			die "unsupported architecture $(uname -m)"
			;;
	esac
}

download_release() {
	need_command tar
	local arch base_url url tmp archive
	arch="$(detect_arch)"
	base_url="${RHUMBASE_RELEASE_BASE_URL:-https://github.com/iketiunn/rhumbase/releases/download}"
	url="${base_url%/}/${RHUMBASE_TAG}/rhumbase_${RHUMBASE_TAG}_linux_${arch}.tar.gz"
	tmp="$(mktemp -d)"
	archive="$tmp/rhumbase.tar.gz"

	if command -v curl >/dev/null 2>&1; then
		run curl -fsSL "$url" -o "$archive"
	elif command -v wget >/dev/null 2>&1; then
		run wget -qO "$archive" "$url"
	else
		die "curl or wget is required to download Rhumbase binaries"
	fi

	run tar -xzf "$archive" -C "$tmp"
	SOURCE_BIN_DIR="$tmp"
}

install_binaries() {
	local bin_dir_actual source bin
	bin_dir_actual="$(prefix_path "$INSTALL_BIN_DIR")"
	if [ -z "$SOURCE_BIN_DIR" ]; then
		download_release
	fi
	source="${SOURCE_BIN_DIR%/}"

	for bin in rhumbase rhumbased; do
		if [ ! -x "$source/$bin" ]; then
			die "$source/$bin is required and must be executable"
		fi
		run cp "$source/$bin" "$bin_dir_actual/$bin"
		run chmod 0755 "$bin_dir_actual/$bin"
	done
}

write_systemd_unit() {
	local unit_path
	unit_path="$(prefix_path "$SYSTEMD_DIR/rhumbased.service")"
	cat > "$unit_path" <<UNIT
[Unit]
Description=Rhumbase daemon
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
Type=simple
User=$DAEMON_USER
Group=$DAEMON_USER
Environment=RHUMBASE_DATA_DIR=$DATA_DIR
Environment=RHUMBASE_GIT_HOST=$GIT_HOST
Environment=RHUMBASE_COMPOSE_RUNNER=docker
Environment=RHUMBASE_CADDY_CONFIG_PATH=$CADDY_CONFIG_PATH
ExecStart=$INSTALL_BIN_DIR/rhumbased
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
UNIT
	run chmod 0644 "$unit_path"
}

verify_installed_binaries() {
	local bin_dir_actual
	bin_dir_actual="$(prefix_path "$INSTALL_BIN_DIR")"
	run "$bin_dir_actual/rhumbase" version >/dev/null
	run "$bin_dir_actual/rhumbased" version >/dev/null
}

reload_systemd() {
	if [ "$SKIP_SYSTEMD_RELOAD" = "1" ]; then
		return
	fi
	run systemctl daemon-reload
	run systemctl enable --now rhumbased.service
}

need_env RHUMBASE_TAG

BOOTSTRAP_ROOT="${RHUMBASE_BOOTSTRAP_ROOT:-/}"
SOURCE_BIN_DIR="${RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR:-}"
INSTALL_BIN_DIR="${RHUMBASE_INSTALL_BIN_DIR:-/usr/local/bin}"
DATA_DIR="${RHUMBASE_DATA_DIR:-/var/lib/rhumbase}"
APPS_DIR="${RHUMBASE_APPS_DIR:-$DATA_DIR/apps}"
SYSTEMD_DIR="${RHUMBASE_SYSTEMD_DIR:-/etc/systemd/system}"
CADDY_CONFIG_PATH="${RHUMBASE_CADDY_CONFIG_PATH:-/etc/caddy/rhumbase.caddyfile}"
GIT_HOST="${RHUMBASE_GIT_HOST:-server}"
DAEMON_USER="${RHUMBASE_DAEMON_USER:-rhumbase}"
SKIP_USER="${RHUMBASE_BOOTSTRAP_SKIP_USER:-0}"
SKIP_CHOWN="${RHUMBASE_BOOTSTRAP_SKIP_CHOWN:-0}"
SKIP_SYSTEMD_RELOAD="${RHUMBASE_BOOTSTRAP_SKIP_SYSTEMD_RELOAD:-0}"

ensure_root_for_real_install
check_dependencies
ensure_daemon_user
prepare_directories
install_binaries
write_systemd_unit
verify_installed_binaries
reload_systemd

printf 'Rhumbase %s installed\n' "$RHUMBASE_TAG"

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

command_works() {
	local name="$1"
	shift
	command -v "$name" >/dev/null 2>&1 && "$name" "$@" >/dev/null 2>&1
}

need_runtime_command() {
	local name="$1"
	shift
	command_works "$name" "$@" || die "$name $* failed; install dependencies or rerun with RHUMBASE_BOOTSTRAP_INSTALL_DEPS=1"
}

check_runtime_dependencies() {
	need_command docker
	need_command caddy
	need_command systemctl
	need_command git
	need_command ssh
	need_command sshd

	run docker version >/dev/null
	run docker compose version >/dev/null
	run caddy version >/dev/null
	run systemctl --version >/dev/null
	run git --version >/dev/null
	run ssh -V >/dev/null 2>&1 || true
	run sshd -V >/dev/null 2>&1 || true
}

detect_deb_arch() {
	need_command dpkg
	local arch
	arch="$(dpkg --print-architecture)"
	case "$arch" in
		amd64|arm64)
			printf '%s\n' "$arch"
			;;
		*)
			die "unsupported architecture $arch; Rhumbase bootstrap supports amd64 and arm64"
			;;
	esac
}

detect_os_release() {
	local os_release tmp
	if [ -n "${RHUMBASE_BOOTSTRAP_TEST_OS_RELEASE:-}" ]; then
		tmp="$(mktemp)"
		printf '%s\n' "$RHUMBASE_BOOTSTRAP_TEST_OS_RELEASE" > "$tmp"
		os_release="$tmp"
	else
		os_release="/etc/os-release"
	fi

	if [ ! -r "$os_release" ]; then
		die "cannot read $os_release"
	fi

	# shellcheck disable=SC1090
	. "$os_release"
	DETECTED_OS_ID="${ID:-}"
	DETECTED_OS_CODENAME="${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}"
	if [ -z "$DETECTED_OS_ID" ]; then
		die "cannot detect OS ID from $os_release"
	fi

	case "$DETECTED_OS_ID" in
		ubuntu|debian)
			;;
		*)
			die "unsupported OS $DETECTED_OS_ID; Rhumbase bootstrap supports Ubuntu LTS and Debian stable"
			;;
	esac

	if [ -z "$DETECTED_OS_CODENAME" ]; then
		die "cannot detect OS codename from $os_release"
	fi
}

deb_source_path() {
	local path="$1"
	prefix_path "$path"
}

write_docker_apt_source() {
	detect_os_release
	local arch keyring source_path repo_os
	arch="$(detect_deb_arch)"
	repo_os="$DETECTED_OS_ID"
	keyring="$(deb_source_path /etc/apt/keyrings/docker.asc)"
	source_path="$(deb_source_path /etc/apt/sources.list.d/docker.sources)"

	run mkdir -p "$(dirname "$keyring")" "$(dirname "$source_path")"
	run curl -fsSL "https://download.docker.com/linux/$repo_os/gpg" -o "$keyring"
	run chmod a+r "$keyring"

	cat > "$source_path" <<SOURCE
Types: deb
URIs: https://download.docker.com/linux/$repo_os
Suites: $DETECTED_OS_CODENAME
Components: stable
Architectures: $arch
Signed-By: /etc/apt/keyrings/docker.asc
SOURCE
	run chmod 0644 "$source_path"
}

write_caddy_apt_source() {
	local tmp keyring source_path
	tmp="$(prefix_path /tmp/rhumbase-caddy-stable.gpg.key)"
	keyring="$(deb_source_path /usr/share/keyrings/caddy-stable-archive-keyring.gpg)"
	source_path="$(deb_source_path /etc/apt/sources.list.d/caddy-stable.list)"

	run mkdir -p "$(dirname "$tmp")" "$(dirname "$keyring")" "$(dirname "$source_path")"
	run curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key -o "$tmp"
	run gpg --batch --yes --dearmor -o "$keyring" "$tmp"
	run chmod o+r "$keyring"
	run curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt -o "$source_path"
	run chmod o+r "$source_path"
}

has_conflicting_docker_packages() {
	local pkg
	for pkg in docker.io docker-compose docker-compose-v2 docker-doc podman-docker containerd runc; do
		if dpkg -s "$pkg" >/dev/null 2>&1; then
			printf '%s\n' "$pkg"
			return 0
		fi
	done
	return 1
}

install_dependencies() {
	need_command apt-get
	need_command curl
	need_command dpkg
	need_command systemctl
	detect_os_release

	run apt-get update
	run apt-get install -y ca-certificates curl gnupg git openssh-server debian-keyring debian-archive-keyring apt-transport-https

	if ! command_works docker version || ! command_works docker compose version; then
		local conflict
		conflict="$(has_conflicting_docker_packages || true)"
		if [ -n "$conflict" ]; then
			die "conflicting Docker package $conflict is installed but Docker is not working; remove or repair it before running bootstrap"
		fi
		write_docker_apt_source
		run apt-get update
		run apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
	fi

	if ! command_works caddy version; then
		write_caddy_apt_source
		run apt-get update
		run apt-get install -y caddy
	fi

	run systemctl enable --now docker
	run systemctl enable --now ssh
	run systemctl enable --now caddy

	need_runtime_command docker version
	need_runtime_command docker compose version
	need_runtime_command caddy version
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
	ensure_system_user "$DAEMON_USER" "$DATA_DIR" "/usr/sbin/nologin"
}

ensure_git_user() {
	if [ "$SKIP_USER" = "1" ]; then
		return
	fi
	ensure_system_user "$GIT_USER" "$GIT_HOME_DIR" "$GIT_SHELL"
}

ensure_system_user() {
	local user="$1"
	local home="$2"
	local shell="$3"
	if id -u "$user" >/dev/null 2>&1; then
		return
	fi
	if [ "$(id -u)" -ne 0 ]; then
		die "user $user is missing; run as root to create it"
	fi
	need_command useradd
	run useradd --system --home "$home" --shell "$shell" "$user"
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

ensure_daemon_docker_access() {
	if [ "$SKIP_USER" = "1" ]; then
		return
	fi
	if [ "$BOOTSTRAP_ROOT" != "/" ] && [ "$INSTALL_DEPS" = "0" ]; then
		return
	fi
	need_command usermod
	run usermod -aG docker "$DAEMON_USER"
}

normalize_runtime_ownership() {
	local data_dir_actual git_home_actual git_ssh_dir_actual git_authorized_keys_actual
	data_dir_actual="$(prefix_path "$DATA_DIR")"
	git_home_actual="$(prefix_path "$GIT_HOME_DIR")"
	git_ssh_dir_actual="$(dirname "$(prefix_path "$GIT_AUTHORIZED_KEYS_PATH")")"
	git_authorized_keys_actual="$(prefix_path "$GIT_AUTHORIZED_KEYS_PATH")"

	run chmod 0755 "$data_dir_actual" "$(prefix_path "$APPS_DIR")" "$git_home_actual"
	run chmod 0700 "$git_ssh_dir_actual" "$(dirname "$(prefix_path "$DASHBOARD_AUTHORIZED_KEYS_PATH")")"

	if [ "$SKIP_CHOWN" = "1" ]; then
		return
	fi
	if [ "$(id -u)" -ne 0 ]; then
		die "cannot set ownership for $data_dir_actual without root"
	fi

	run chown -R "$DAEMON_USER:$DAEMON_USER" "$data_dir_actual"
	run chown -R "$GIT_USER:$GIT_USER" "$git_home_actual"
	run chmod 0755 "$git_home_actual"
	run chmod 0700 "$git_ssh_dir_actual"
	run touch "$git_authorized_keys_actual"
	run chmod 0600 "$git_authorized_keys_actual"
	run chown "$GIT_USER:$GIT_USER" "$git_ssh_dir_actual" "$git_authorized_keys_actual"
}

prepare_directories() {
	local bin_dir_actual data_dir_actual apps_dir_actual systemd_dir_actual caddy_dir_actual
	local git_home_actual git_ssh_dir_actual dashboard_dir_actual
	bin_dir_actual="$(prefix_path "$INSTALL_BIN_DIR")"
	data_dir_actual="$(prefix_path "$DATA_DIR")"
	apps_dir_actual="$(prefix_path "$APPS_DIR")"
	systemd_dir_actual="$(prefix_path "$SYSTEMD_DIR")"
	caddy_dir_actual="$(dirname "$(prefix_path "$CADDY_CONFIG_PATH")")"
	git_home_actual="$(prefix_path "$GIT_HOME_DIR")"
	git_ssh_dir_actual="$(dirname "$(prefix_path "$GIT_AUTHORIZED_KEYS_PATH")")"
	dashboard_dir_actual="$(dirname "$(prefix_path "$DASHBOARD_AUTHORIZED_KEYS_PATH")")"

	run mkdir -p "$bin_dir_actual" "$data_dir_actual" "$apps_dir_actual" "$systemd_dir_actual" "$caddy_dir_actual" "$git_home_actual" "$git_ssh_dir_actual" "$dashboard_dir_actual"
	normalize_runtime_ownership
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
Environment=RHUMBASE_SSH_LISTEN_ADDR=$SSH_LISTEN_ADDR
Environment=RHUMBASE_DASHBOARD_USER=$DASHBOARD_USER
Environment=RHUMBASE_DASHBOARD_HOST_KEY_PATH=$DASHBOARD_HOST_KEY_PATH
Environment=RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH=$DASHBOARD_AUTHORIZED_KEYS_PATH
Environment=RHUMBASE_GIT_HOST=$GIT_HOST
Environment=RHUMBASE_GIT_USER=$GIT_USER
Environment=RHUMBASE_GIT_HOME_DIR=$GIT_HOME_DIR
Environment=RHUMBASE_GIT_AUTHORIZED_KEYS_PATH=$GIT_AUTHORIZED_KEYS_PATH
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

configure_caddy_import() {
	local generated_actual caddy_main_actual import_line
	generated_actual="$(prefix_path "$CADDY_CONFIG_PATH")"
	caddy_main_actual="$(prefix_path "$CADDY_MAIN_CONFIG_PATH")"
	import_line="import $CADDY_CONFIG_PATH"

	run mkdir -p "$(dirname "$generated_actual")" "$(dirname "$caddy_main_actual")"
	if [ ! -f "$generated_actual" ]; then
		printf '# Rhumbase generated routes\n' > "$generated_actual"
	fi
	run chmod 0644 "$generated_actual"

	if [ ! -f "$caddy_main_actual" ]; then
		printf '# Caddy config managed by the system administrator.\n%s\n' "$import_line" > "$caddy_main_actual"
	elif ! grep -Fqx "$import_line" "$caddy_main_actual"; then
		if [ ! -f "$caddy_main_actual.rhumbase.bak" ]; then
			run cp "$caddy_main_actual" "$caddy_main_actual.rhumbase.bak"
		fi
		printf '\n%s\n' "$import_line" >> "$caddy_main_actual"
	fi
	run chmod 0644 "$caddy_main_actual"
}

reload_caddy() {
	if [ "$SKIP_SYSTEMD_RELOAD" = "1" ]; then
		return
	fi
	local caddy_main_actual
	caddy_main_actual="$(prefix_path "$CADDY_MAIN_CONFIG_PATH")"
	run caddy validate --config "$caddy_main_actual" >/dev/null
	run systemctl reload caddy || run systemctl restart caddy
}

need_env RHUMBASE_TAG

BOOTSTRAP_ROOT="${RHUMBASE_BOOTSTRAP_ROOT:-/}"
SOURCE_BIN_DIR="${RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR:-}"
INSTALL_BIN_DIR="${RHUMBASE_INSTALL_BIN_DIR:-/usr/local/bin}"
DATA_DIR="${RHUMBASE_DATA_DIR:-/var/lib/rhumbase}"
APPS_DIR="${RHUMBASE_APPS_DIR:-$DATA_DIR/apps}"
SYSTEMD_DIR="${RHUMBASE_SYSTEMD_DIR:-/etc/systemd/system}"
CADDY_CONFIG_PATH="${RHUMBASE_CADDY_CONFIG_PATH:-/etc/caddy/rhumbase.caddyfile}"
CADDY_MAIN_CONFIG_PATH="${RHUMBASE_CADDY_MAIN_CONFIG_PATH:-/etc/caddy/Caddyfile}"
SSH_LISTEN_ADDR="${RHUMBASE_SSH_LISTEN_ADDR:-:2222}"
DASHBOARD_USER="${RHUMBASE_DASHBOARD_USER:-dashboard}"
DASHBOARD_HOST_KEY_PATH="${RHUMBASE_DASHBOARD_HOST_KEY_PATH:-$DATA_DIR/dashboard/ssh_host_rsa_key}"
DASHBOARD_AUTHORIZED_KEYS_PATH="${RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH:-$DATA_DIR/dashboard/authorized_keys}"
GIT_HOST="${RHUMBASE_GIT_HOST:-server}"
GIT_USER="${RHUMBASE_GIT_USER:-git}"
GIT_HOME_DIR="${RHUMBASE_GIT_HOME_DIR:-$DATA_DIR/git}"
GIT_AUTHORIZED_KEYS_PATH="${RHUMBASE_GIT_AUTHORIZED_KEYS_PATH:-$GIT_HOME_DIR/.ssh/authorized_keys}"
GIT_SHELL="${RHUMBASE_GIT_SHELL:-/usr/bin/git-shell}"
DAEMON_USER="${RHUMBASE_DAEMON_USER:-rhumbase}"
SKIP_USER="${RHUMBASE_BOOTSTRAP_SKIP_USER:-0}"
SKIP_CHOWN="${RHUMBASE_BOOTSTRAP_SKIP_CHOWN:-0}"
SKIP_SYSTEMD_RELOAD="${RHUMBASE_BOOTSTRAP_SKIP_SYSTEMD_RELOAD:-0}"
INSTALL_DEPS="${RHUMBASE_BOOTSTRAP_INSTALL_DEPS:-}"
if [ -z "$INSTALL_DEPS" ]; then
	if [ "$BOOTSTRAP_ROOT" = "/" ]; then
		INSTALL_DEPS=1
	else
		INSTALL_DEPS=0
	fi
fi
case "$INSTALL_DEPS" in
	0|1)
		;;
	*)
		die "RHUMBASE_BOOTSTRAP_INSTALL_DEPS must be 0 or 1"
		;;
esac

ensure_root_for_real_install
if [ "$INSTALL_DEPS" = "1" ]; then
	install_dependencies
else
	check_runtime_dependencies
fi
ensure_daemon_user
ensure_git_user
prepare_directories
ensure_daemon_docker_access
install_binaries
write_systemd_unit
configure_caddy_import
verify_installed_binaries
reload_caddy
reload_systemd

printf 'Rhumbase %s installed\n' "$RHUMBASE_TAG"

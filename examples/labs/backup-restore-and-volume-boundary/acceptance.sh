#!/usr/bin/env bash

set -euo pipefail

: "${SSHDOCK_TARGET:?set SSHDOCK_TARGET to sshdock@your-server}"
: "${SSHDOCK_ADMIN_TARGET:?set SSHDOCK_ADMIN_TARGET to an administrator SSH target}"
: "${SSHDOCK_ROUTE_HOST:?set SSHDOCK_ROUTE_HOST to the WordPress app hostname}"
: "${SSHDOCK_BACKUP_LAB_SECRET:?set SSHDOCK_BACKUP_LAB_SECRET to the configured BACKUP_LAB_SECRET}"

APP=${SSHDOCK_APP:-backup-restore-and-volume-boundary}
ARCHIVE=${SSHDOCK_BACKUP_PATH:-/root/sshdock-backup-lab.tar.gz}
VOLUME_PREFIX=${SSHDOCK_VOLUME_PREFIX:-sshdock_${APP}}
SSH_ARGS=()
if [[ -n ${SSHDOCK_IDENTITY_FILE:-} ]]; then
	SSH_ARGS=(-i "$SSHDOCK_IDENTITY_FILE")
fi

ssh_to() {
	local target=$1
	shift
	ssh -T "${SSH_ARGS[@]}" -- "$target" "$(remote_command "$@")"
}

remote_command() {
	local argument command=""
	for argument; do
		command+="$(quote_remote_argument "$argument") "
	done
	printf '%s' "${command% }"
}

quote_remote_argument() {
	local value=$1
	value=${value//\'/\'\"\'\"\'}
	printf "'%s'" "$value"
}

operator() {
	ssh_to "$SSHDOCK_TARGET" "$@"
}

admin() {
	ssh_to "$SSHDOCK_ADMIN_TARGET" "$@"
}

expect_rejection() {
	local expected=$1
	shift
	set +e
	output=$("$@" 2>&1)
	status=$?
	set -e
	printf '%s\n' "$output"
	if [[ $status -eq 0 || $output != *"$expected"* ]]; then
		echo "expected rejection containing $expected" >&2
		exit 1
	fi
}

wait_for_healthy_app() {
	local attempt health
	for attempt in {1..30}; do
		set +e
		health=$(operator apps health "$APP" 2>&1)
		status=$?
		set -e
		if [[ $status -eq 0 ]] && grep -Fx "health: ok" <<<"$health" >/dev/null; then
			printf '%s\n' "$health"
			return
		fi
		sleep 2
	done
	echo "app did not become healthy within 60 seconds" >&2
	exit 1
}

daemon_stopped=0
start_daemon_on_exit() {
	if [[ $daemon_stopped -eq 1 ]]; then
		admin sudo systemctl start sshdockd
	fi
}
trap start_daemon_on_exit EXIT

wait_for_healthy_app
before_domains=$(operator domains list "$APP")
printf '%s\n' "$before_domains"
grep -F "${SSHDOCK_ROUTE_HOST}"$'\tweb\t18200\ttrue' <<<"$before_domains"

created=$(admin sudo sshdock backup create --output "$ARCHIVE")
printf '%s\n' "$created"
grep -F "created backup $ARCHIVE" <<<"$created"

inspection=$(admin sudo sshdock backup inspect "$ARCHIVE")
printf '%s\n' "$inspection"
grep -F "format: sshdock-backup/v1" <<<"$inspection"
grep -F "Docker volumes:" <<<"$inspection"

archive_entries=$(admin sudo tar -tzf "$ARCHIVE")
for entry in \
	manifest.json \
	data/sshdock.db \
	data/config.key \
	"data/apps/$APP/repo.git/HEAD" \
	"data/apps/$APP/worktree/compose.yml" \
	data/git/.ssh/authorized_keys \
	data/.ssh/authorized_keys \
	caddy/generated.caddyfile \
	caddy/main.Caddyfile \
	docker/volumes.json; do
	grep -Fx "$entry" <<<"$archive_entries"
done

expect_rejection "not implemented" admin sudo sshdock backup create --include-volumes --output "$ARCHIVE.with-volumes"

admin sudo systemctl stop sshdockd
daemon_stopped=1
admin sudo sshdock backup restore "$ARCHIVE"
admin sudo sshdock diagnostics
admin sudo systemctl start sshdockd
daemon_stopped=0

restored_secret=$(operator config get "$APP" BACKUP_LAB_SECRET)
if [[ $restored_secret != "$SSHDOCK_BACKUP_LAB_SECRET" ]]; then
	echo "restored config does not decrypt BACKUP_LAB_SECRET" >&2
	exit 1
fi

wait_for_healthy_app
after_domains=$(operator domains list "$APP")
printf '%s\n' "$after_domains"
grep -F "${SSHDOCK_ROUTE_HOST}"$'\tweb\t18200\ttrue' <<<"$after_domains"
route_check=$(operator domains check "$APP")
printf '%s\n' "$route_check"
grep -Fx "${SSHDOCK_ROUTE_HOST}"$'\tweb\t18200\ttrue\tok\tactive Caddy route matches' <<<"$route_check"
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 "https://${SSHDOCK_ROUTE_HOST}" >/dev/null
admin sudo docker volume inspect "${VOLUME_PREFIX}_wordpress-data" "${VOLUME_PREFIX}_mariadb-data"

#!/usr/bin/env bash

set -euo pipefail

: "${SSHDOCK_TARGET:?set SSHDOCK_TARGET to sshdock@your-server}"
: "${SSHDOCK_ROUTE_HOST:?set SSHDOCK_ROUTE_HOST to the recovered app hostname}"
: "${SSHDOCK_EXPECTED_MAIN:?set SSHDOCK_EXPECTED_MAIN to the known-good commit}"
: "${SSHDOCK_FAILED_MAIN:?set SSHDOCK_FAILED_MAIN to the controlled failed commit}"

APP=${SSHDOCK_APP:-failed-deploy-and-git-recovery}
SSH_ARGS=()
if [[ -n ${SSHDOCK_IDENTITY_FILE:-} ]]; then
	SSH_ARGS=(-i "$SSHDOCK_IDENTITY_FILE")
fi

operator() {
	if ((${#SSH_ARGS[@]})); then
		ssh -T "${SSH_ARGS[@]}" "$SSHDOCK_TARGET" "$@"
		return
	fi
	ssh -T "$SSHDOCK_TARGET" "$@"
}

count_rows() {
	awk 'NF { count++ } END { print count + 0 }'
}

assert_health() {
	local output=$1
	printf '%s\n' "$output"
	grep -Fx "current main: $SSHDOCK_EXPECTED_MAIN" <<<"$output"
	grep -F "latest deploy:" <<<"$output"
	grep -F "succeeded commit=$SSHDOCK_EXPECTED_MAIN" <<<"$output"
	grep -Fx "routes: 1 active, 0 attention" <<<"$output"
	grep -F $'ok\trestart policy\t' <<<"$output"
	grep -F "last failure:" <<<"$output"
}

assert_active_route() {
	local domains check
	domains=$(operator domains list "$APP")
	printf '%s\n' "$domains"
	grep -F "${SSHDOCK_ROUTE_HOST}"$'\tweb\t18100\ttrue' <<<"$domains"

	check=$(operator domains check "$APP")
	printf '%s\n' "$check"
	grep -Fx "${SSHDOCK_ROUTE_HOST}"$'\tweb\t18100\ttrue\tok\tactive Caddy route matches' <<<"$check"
}

assert_releases() {
	local releases=$1
	printf '%s\n' "$releases"
	grep -F "$SSHDOCK_EXPECTED_MAIN" <<<"$releases"
	grep -F "$SSHDOCK_FAILED_MAIN" <<<"$releases"
}

assert_deployments() {
	local deployments=$1
	printf '%s\n' "$deployments"
	grep -F $'failed\tpush\t' <<<"$deployments"
	grep -F "$SSHDOCK_FAILED_MAIN" <<<"$deployments"
	grep -F "$SSHDOCK_EXPECTED_MAIN" <<<"$deployments"
}


assert_events() {
	local events=$1
	printf '%s\n' "$events"
	grep -F "deploy.failed" <<<"$events"
	grep -F "deploy.succeeded" <<<"$events"
}

assert_health "$(operator apps health "$APP")"
assert_active_route
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 "https://${SSHDOCK_ROUTE_HOST}" >/dev/null

operator logs "$APP" web --tail 20

follow_file=$(mktemp)
trap 'rm -f "$follow_file"' EXIT
operator logs "$APP" web --tail 20 -f >"$follow_file" 2>&1 &
follow_pid=$!
sleep 5
if kill -0 "$follow_pid" 2>/dev/null; then
	kill "$follow_pid"
fi
set +e
wait "$follow_pid"
follow_status=$?
set -e
follow_output=$(<"$follow_file")
printf '%s\n' "$follow_output"
if ((follow_status != 0 && follow_status != 143)); then
	echo "log follow failed with status $follow_status" >&2
	exit 1
fi
if [[ -z $follow_output ]]; then
	echo "log follow produced no output" >&2
	exit 1
fi

releases_before=$(operator releases list "$APP")
deployments_before=$(operator deployments list "$APP")
events_before=$(operator events list "$APP")
assert_releases "$releases_before"
assert_deployments "$deployments_before"
assert_events "$events_before"
releases_before_count=$(count_rows <<<"$releases_before")
deployments_before_count=$(count_rows <<<"$deployments_before")

operator apps redeploy "$APP"
assert_health "$(operator apps health "$APP")"
assert_active_route
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 "https://${SSHDOCK_ROUTE_HOST}" >/dev/null

releases_after=$(operator releases list "$APP")
deployments_after=$(operator deployments list "$APP")
events_after=$(operator events list "$APP")
assert_releases "$releases_after"
assert_deployments "$deployments_after"
assert_events "$events_after"
releases_after_count=$(count_rows <<<"$releases_after")
deployments_after_count=$(count_rows <<<"$deployments_after")
if ((releases_after_count != releases_before_count)); then
	echo "release rows = $releases_after_count, want $releases_before_count after redeploy" >&2
	exit 1
fi
if ((deployments_after_count != deployments_before_count + 1)); then
	echo "deployment rows = $deployments_after_count, want $((deployments_before_count + 1)) after redeploy" >&2
	exit 1
fi
grep -F $'succeeded\tredeploy\t'"$SSHDOCK_EXPECTED_MAIN" <<<"$deployments_after"

operator apps remove "$APP" --force

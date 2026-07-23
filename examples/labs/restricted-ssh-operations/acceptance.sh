#!/usr/bin/env bash
set -euo pipefail

: "${SSHDOCK_TARGET:?set SSHDOCK_TARGET to sshdock@your-server}"
: "${SSHDOCK_ADMIN_TARGET:?set SSHDOCK_ADMIN_TARGET to an administrator SSH target}"
: "${SSHDOCK_ROUTE_HOST:?set SSHDOCK_ROUTE_HOST to the app hostname}"

APP=restricted-ssh-operations
ROUTE_HOST=$SSHDOCK_ROUTE_HOST
VOLUME=sshdock_${APP}_laravel_storage
SSH_ARGS=()
if [[ -n ${SSHDOCK_IDENTITY_FILE:-} ]]; then
  SSH_ARGS=(-i "$SSHDOCK_IDENTITY_FILE")
fi

operator() {
  ssh "${SSH_ARGS[@]}" "$SSHDOCK_TARGET" "$1"
}

assert_active_route() {
  route_check=$(operator "domains check $APP")
  printf '%s\n' "$route_check"
  if [[ $route_check != *"$ROUTE_HOST"* || $route_check != *"active Caddy route matches"* ]]; then
    echo "route did not become active" >&2
    exit 1
  fi
}

admin() {
  ssh "${SSH_ARGS[@]}" "$SSHDOCK_ADMIN_TARGET" "$1"
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

operator "apps health $APP"
admin "sudo sshdock domains attach $APP web $ROUTE_HOST --port 18102"
assert_active_route
operator "apps exec $APP web -- php artisan about --only 'Application Name'"
operator "apps run $APP web -- php artisan migrate --force"
ssh -tt "${SSH_ARGS[@]}" "$SSHDOCK_TARGET" "apps exec $APP web -- php artisan about"
operator "apps stop $APP"
expect_rejection "not running" operator "apps exec $APP web -- php artisan about"
operator "apps start $APP"
operator "apps restart $APP"
operator "apps redeploy $APP"
assert_active_route
expect_rejection "not available over SSH" ssh -T "${SSH_ARGS[@]}" "$SSHDOCK_TARGET" hostname
operator "apps remove $APP --force"
expect_rejection "not found" operator "apps health $APP"
route_state=$(admin "if sudo grep -F -- '$ROUTE_HOST' /etc/caddy/sshdock/sshdock.caddyfile; then printf route-present; else printf route-absent; fi")
if [[ $route_state != route-absent ]]; then
  echo "route remains after removal" >&2
  exit 1
fi
admin "sudo docker volume inspect $VOLUME >/dev/null"

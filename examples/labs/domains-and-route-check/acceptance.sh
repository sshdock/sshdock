#!/usr/bin/env bash

set -euo pipefail

: "${SSHDOCK_TARGET:?set SSHDOCK_TARGET to sshdock@your-server}"
: "${SSHDOCK_ADMIN_TARGET:?set SSHDOCK_ADMIN_TARGET to an administrator SSH target}"
: "${SSHDOCK_AUTO_ROUTE_HOST:?set SSHDOCK_AUTO_ROUTE_HOST to domains-and-route-check.your-domain}"
: "${SSHDOCK_MANUAL_ROUTE_HOST:?set SSHDOCK_MANUAL_ROUTE_HOST to a second routed hostname}"

APP=${SSHDOCK_APP:-domains-and-route-check}
PORT=18200
SSH_ARGS=()
if [[ -n ${SSHDOCK_IDENTITY_FILE:-} ]]; then
  SSH_ARGS=(-i "$SSHDOCK_IDENTITY_FILE")
fi

ssh_to() {
  local target=$1
  shift
  if ((${#SSH_ARGS[@]})); then
    ssh -T "${SSH_ARGS[@]}" "$target" "$@"
    return
  fi
  ssh -T "$target" "$@"
}

active_route_row() {
  printf '%s\tweb\t%s\ttrue\tok\tactive Caddy route matches' "$1" "$PORT"
}

operator() {
  ssh_to "$SSHDOCK_TARGET" "$@"
}

admin() {
  ssh_to "$SSHDOCK_ADMIN_TARGET" "$@"
}

auto_domains=$(operator domains list "$APP")
printf '%s\n' "$auto_domains"
grep -F "${SSHDOCK_AUTO_ROUTE_HOST}"$'\tweb\t18200\ttrue' <<<"$auto_domains"
if grep -F $'\tdb\t' <<<"$auto_domains"; then
  echo "the private database service received a public route" >&2
  exit 1
fi

operator apps health "$APP"
auto_check=$(operator domains check "$APP")
printf '%s\n' "$auto_check"
grep -Fx "$(active_route_row "$SSHDOCK_AUTO_ROUTE_HOST")" <<<"$auto_check"
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 "https://${SSHDOCK_AUTO_ROUTE_HOST}" >/dev/null

admin "sudo sshdock domains attach $APP web $SSHDOCK_MANUAL_ROUTE_HOST --port $PORT"
manual_check=$(operator domains check "$APP")
printf '%s\n' "$manual_check"
grep -Fx "$(active_route_row "$SSHDOCK_MANUAL_ROUTE_HOST")" <<<"$manual_check"

unavailable_check=$(admin "sudo env SSHDOCK_CADDY_ADMIN_ADDRESS=127.0.0.1:1 sshdock domains check $APP")
printf '%s\n' "$unavailable_check"
grep -F "active Caddy check failed" <<<"$unavailable_check"

admin "sudo sshdock domains detach $APP $SSHDOCK_MANUAL_ROUTE_HOST"
after_manual_detach=$(operator domains check "$APP")
printf '%s\n' "$after_manual_detach"
grep -Fx "$(active_route_row "$SSHDOCK_AUTO_ROUTE_HOST")" <<<"$after_manual_detach"
if grep -F "$SSHDOCK_MANUAL_ROUTE_HOST" <<<"$after_manual_detach"; then
  echo "manual route remains active after detach" >&2
  exit 1
fi
admin "sudo sshdock domains detach $APP $SSHDOCK_AUTO_ROUTE_HOST"
final_domains=$(operator domains list "$APP")
printf '%s\n' "$final_domains"
grep -Fx "no domains" <<<"$final_domains"
admin "sudo caddy validate --config /etc/caddy/Caddyfile"

operator apps health "$APP"
operator apps remove "$APP" --force

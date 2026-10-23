# Structured inspection

This contract is a **v1.1.0** addition. Published v1.0.0 binaries do not support it. Default human-readable commands and their exit behavior remain compatible.

Append `--json` once, at the end of a supported read-only command:

```bash
ssh sshdock@sshdock.example.com apps list --json
ssh sshdock@sshdock.example.com apps info my-app --json
ssh sshdock@sshdock.example.com apps health my-app --json
ssh sshdock@sshdock.example.com deployments list my-app --json
ssh sshdock@sshdock.example.com deployments info my-app dep_123 --json
ssh sshdock@sshdock.example.com releases list my-app --json
ssh sshdock@sshdock.example.com domains list my-app --json
ssh sshdock@sshdock.example.com domains check my-app --json
ssh sshdock@sshdock.example.com events list my-app --json
sudo sshdock diagnostics --json
sshdock check ./my-app --json
ssh sshdock@sshdock.example.com apps check my-app --json
```

All listed app inspection commands also work locally. `diagnostics` remains host administration and is unavailable over restricted SSH; `check [directory]` inspects a local project. Mutations, config values, and streaming logs do not accept this output flag. A container command's own `--json` after the required `apps exec/run ... --` delimiter is passed through unchanged.

## Envelope and compatibility

Each query writes one JSON object and newline to stdout:

```json
{"schema_version":"1","command":"deployments.info","data":{"id":"dep_123","app_name":"my-app","release_id":"rel_abc","commit_sha":"abc","trigger":"push","status":"pending","started_at":"2026-10-23T01:00:00Z","failure_stage":"","failure_detail":"","retry_guidance":""},"error":null}
```

`schema_version` is the string `"1"`. `command` uses a dot between group and action, or `diagnostics`. `data` is the result; `error` is null on a successful query. Fields use lower snake case. Lists, including nested service/check lists, are always arrays (`[]` when empty). Timestamps use RFC 3339 with optional fractional seconds. An unset `finished_at` is omitted; unavailable identities are empty strings. Do not treat an empty identity or missing completion timestamp as a successful deployment.

Consumers must check the schema version, ignore unknown fields, and handle unknown future state values conservatively. Additive fields are compatible within version 1; removing a field or changing its meaning/type requires a new schema version. Invalid commands use the recognized command group or `unknown` when no command can be identified.

```json
{"schema_version":"1","command":"deployments.info","data":null,"error":{"code":"not_found","message":"deployment \"dep_missing\" not found for app \"my-app\""}}
```

Error codes are `invalid_arguments` (unsupported syntax or output flag), `not_found` (missing app or app-scoped deployment), and `unavailable` (store, configuration, runtime, or other query failure). Messages explain the problem but are not a parsing contract. Parseable JSON requests also receive JSON for initialization and restricted-command rejection. SSH transport/authentication failures and malformed SSH quoting can fail before the CLI contract runs; keep stderr separate and check the process exit.

| Exit | Meaning |
| --- | --- |
| `0` | Query completed. The returned deployment may still be pending, deploying, or failed; health/route checks may warn or fail. |
| `1` | Query failed (`error` is non-null), output could not be written, or diagnostics/preflight completed with `data.ok=false`. |
| `2` | Invalid command or arguments. |

On ordinary JSON success/error responses stderr is empty. An output-writing failure can instead be reported there. Diagnostics returns its completed report with `error:null` even when checks fail; use both its exit and `data.ok`.

Preflight behaves the same way for completed blocking reports. `check` and `apps.check` return `ok`, `scope`, `compose_path`, and `checks` with `name`, `status`, `detail`, `fix`, `blocking`. Statuses are `ok`, `warn`, `fail`, `unknown`; `ok:true` only means no confirmed blockers. See [preflight scope and uncertainty](PREFLIGHT.md).

## Result fields

| Command | `data` |
| --- | --- |
| `apps.list`, `apps.info` | Array or one app: `name`, `status`, `node_id`, `default_url`. This is stored lifecycle state, not a current availability probe. |
| `deployments.list`, `deployments.info` | Array or one attempt: `id`, `app_name`, `release_id`, `commit_sha`, `trigger`, `status`, `started_at`, optional `finished_at`, `failure_stage`, `failure_detail`, `retry_guidance`. Detail lookup is scoped to the named app. |
| `releases.list` | Array of `id`, `app_name`, `commit_sha`, `compose_path`, `status`, `failure`, `created_at`, `updated_at`. Release state is historical and does not replace attempt state. |
| `events.list` | Array of `id`, `app_name`, `type`, `message`, `created_at`; removal audit history remains readable after app removal. |
| `domains.list` | Array of `app_name`, `service_name`, `domain_name`, `port`, `https` describing stored routing intent. |
| `domains.check` | Array of `domain_name`, `service_name`, `port`, `https`, `status`, `detail` comparing intent with active Caddy configuration. |
| `diagnostics` | `ok` and `checks`: each has `name`, `ok`, `warning`, `message`, `why`, `fix`. Warning checks can have `ok=true`; inspect `warning` separately. |

Deployment states are `pending`, `deploying`, `succeeded`, and `failed`. Pending/deploying are incomplete; succeeded/failed are terminal. Daemon-interrupted active work is recorded as `failed` with failure/retry details, never silently treated as success or resumed. Triggers are `push`, `redeploy`, or historical `legacy`, `rollback`, `startup_recovery`. Release states are `pending`, `deploying`, `succeeded`, `failed`, and historical `rolled_back`. App states are `created`, `deploying`, `healthy`, `failed`, `unknown`.

Domain-check states are `ok`, `stored`, `unavailable`, `failed`, `missing`, `mismatch`. `stored` means active inspection is unsupported; `unavailable` means inspection failed. Neither proves a working route. Even `ok` only confirms matching active Caddy configuration; it does not test external DNS, TLS, network ingress, or an application response.

## Independent deployment and availability facts

`apps.health` returns stored `app_name`, `status`, `node_id` plus:

- Desired revision: `current_main_commit`, resolved from remote Git `main`; empty means unavailable.
- Latest attempt: `latest_deployment_id`, `latest_deployment_commit`, `latest_deployment_trigger`, `latest_deployment_status`, `attempt_release_id`, `attempt_release_status`.
- Last successful attempt: `last_successful_deployment_id`, `last_successful_deployment_commit`, `last_successful_release_id`. These are selected from successful attempts by start time then ID, including retries and a later successful deployment of an older release. They describe recorded success, not proof those containers are still serving.
- Runtime observations: `service_count`, `running_service_count`, `attention_service_count`, `services` with `name`/`state` from the current worktree's Compose project.
- Routing observations: `domain_count`, `active_route_count`, `route_attention_count`, `route_status`.
- Prior failure context: `last_failure_deployment_id`, `last_failure`.
- `checks` with `name`, `status` (`ok`, `warn`, `fail`, or unknown), `detail`; `health` aggregates those check severities (`ok`, `warn`, `fail`). A mismatch between desired main and the latest attempt produces a separate warning.

Empty histories, missing worktrees, unavailable Git/Caddy/runtime inspection and non-restarting services remain warnings or failures. Running containers, successful history, and matching routes are separate observations; verify the public URL independently. Stored config values are redacted from ordinary failure/event/domain details; deliberately revealed secrets and arbitrary transformed values are outside that guarantee.

## CI polling example

Capture the deployment ID printed as `deploy: queued dep_...` by an accepted push. Git acceptance updates remote main and records the attempt; it does not mean deployment succeeded. If the client detaches, inspect the same ID rather than assuming a newer attempt belongs to that push.

The following uses `jq` on the CI client and a bounded wait:

```bash
set -eu
operator=sshdock@sshdock.example.com
app=my-app
deployment=dep_123 # Exact ID from this push, not an arbitrary latest record.
for attempt in $(seq 1 120); do
  result=$(ssh "$operator" deployments info "$app" "$deployment" --json)
  printf '%s\n' "$result" | jq -e '.schema_version == "1" and .error == null' >/dev/null
  state=$(printf '%s\n' "$result" | jq -r '.data.status')
  case "$state" in
    succeeded) break ;;
    failed) printf '%s\n' "$result" >&2; exit 1 ;;
    pending|deploying) sleep 2 ;;
    *) printf 'Unknown deployment state: %s\n' "$state" >&2; exit 1 ;;
  esac
done
test "$state" = succeeded
ssh "$operator" apps health "$app" --json
ssh "$operator" domains check "$app" --json
curl --fail --show-error https://my-app.example.com/
```

A successful attempt is necessary for this deployment check. The later health/route JSON queries still require inspection of their result fields; their exit `0` only means the queries completed. Network or SSH failures should fail CI without being reclassified as deployment success.

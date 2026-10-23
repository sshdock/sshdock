# Read-only preflight

Preflight is a **v1.1.0** addition. It inspects Compose inputs and observable prerequisites without deploying, fetching images, building, changing Git refs, applying Caddy configuration, starting/stopping containers, or changing stored app config.

Check a proposed local project before pushing:

```bash
sshdock check .
sshdock check ./my-app --json
```

Check an existing server app's current worktree:

```bash
sudo sshdock apps check my-app
ssh sshdock@sshdock.example.com apps check my-app --json
```

The latter reads existing SQLite state without creating or migrating the database. It reads encrypted app config with the existing key; it never creates a missing key. A missing or incompatible store must be repaired through normal installation/upgrade, not through preflight. No release, deployment attempt, event, checkout, or runtime mutation is created by a check.

`apps check` inspects the checked-out worktree used by operational commands, including its checked-out Git metadata. It does not check out a newer queued remote `main`. To inspect a proposed commit before pushing it, check its local directory; local worktree edits are included and are not proof of what a later Git push will contain.

## What results mean

**`ok:true` means no confirmed blockers were found. It does not mean every check completed, deployment will succeed, or the app is publicly available.** Review warnings and unknowns. Automation that requires a particular observation must reject an unknown result for that check.

Each check has `name`, `status`, `detail`, `fix`, and `blocking`:

| Status | Meaning |
| --- | --- |
| `ok` | The stated observation passed within the report's scope. |
| `fail` | A confirmed problem was found; `blocking:true` means it prevents the checked deployment requirements. |
| `warn` | An actionable risk or uncertain conflict; inspect the detail and suggested next action. |
| `unknown` | The condition was not verified or cannot be established from the available evidence. |

A completed report exits `1` if it contains a confirmed blocker and `0` otherwise. Store/app lookup or other query failures exit `1` with a normal structured error; invalid syntax exits `2`. Human output includes the same status, blocker flag, detail and fix as JSON.

## Checks and boundaries

- Exactly one supported root Compose file must exist. Missing/multiple files, invalid YAML, unsupported external files, required interpolation failures, and native Compose rejection produce actionable blockers.
- Native `docker compose config --format json` remains the authority for ordinary Compose semantics. Preflight privately captures its effective model and warnings; it never prints the resolved config or environment. If native Compose is unavailable locally, its validation is marked unknown rather than pretending the target server cannot deploy.
- Local checks use the local project/environment and, when available, a verified local Git HEAD for `SSHDOCK_GIT_SHA`. Process or `.env` spoofing cannot substitute that metadata. A local check cannot establish the actual server's engine, host-port availability or Caddy state.
- Existing-app checks use stored app config and the current managed worktree's metadata. Observable server Docker/Compose prerequisites are checked with read-only commands. Missing/inaccessible prerequisites can block. Config values are redacted from ordinary error/warning details.
- Routing uses the same effective-model inference as deployment. A valid worker may have no HTTP route. Ambiguous/missing routing is a warning with manual routing guidance, not a claim that an otherwise valid Compose application cannot run.
- Server port checks assume Docker Engine runs on the SSHDock node; remote Docker contexts are outside these host-listener observations. Checks distinguish protocol and bind addresses where observable. A confirmed overlapping foreign Docker publisher can block. An observed binding owned by the same app/service is expected occupancy. A listener with uncertain ownership is a possible-conflict warning. Dynamic/ranged assignments and ambiguous address overlap remain unknown; no port is bound or reserved by preflight.
- Server Caddy inspection reports whether its active configuration can be read. This does not prove a later reload, DNS, TLS, firewall/provider ingress, or an application response. Use `domains check`, `diagnostics`, and a public URL check for those separate observations.
- Image/registry credentials, pulls, builds, running-service readiness, database migrations and external dependencies are not exercised. An inaccessible private registry cannot be ruled out by config validation. Fixes remain explicit operator actions; preflight never applies them automatically.

For example, a missing required config value should lead to `config set` followed by another check. A likely port conflict should lead to inspecting the listener/containers and choosing a free binding. Ambiguous routing should lead to an explicit route choice, not a guessed public service.

## JSON contract

Both commands use [schema version 1](JSON_OUTPUT.md). Commands are `check` and `apps.check`; a completed report is in `data` with `error:null`, including when blocking checks make the exit nonzero:

```json
{"schema_version":"1","command":"check","data":{"ok":false,"scope":"local","compose_path":"","checks":[{"name":"compose file","status":"fail","detail":"no supported root Compose file","fix":"add exactly one supported root Compose file","blocking":true}]},"error":null}
```

The report contains `ok`, `scope` (`local` or `server`), `compose_path`, and an array of `checks`. Fields may be added compatibly; check the envelope version and handle unknown future statuses conservatively. Empty/unavailable metadata is not a passing observation. Keep stderr separate from JSON stdout, especially across SSH transport failures.

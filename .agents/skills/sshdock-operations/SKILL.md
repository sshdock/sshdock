---
name: sshdock-operations
license: Apache-2.0
description: Deploy and operate a trusted owner's SSHDock Compose apps with Git push and SSH; inspect status, diagnose failures, and guide approved manual recovery or data backup. Use for SSHDock operations, not generic SSH administration.
---

# SSHDock operations

Use the OSS Git/SSH/CLI contract; no cloud account or agent service is required. These instructions require v1.1 commands (unreleased until v1.1 publication). Published v1.0.0 lacks structured inspection, preflight and volume-data commands. Confirm server/client capabilities; stop on unsupported commands rather than inventing replacements.

The owner supplies the intended host, operator identity, trusted local repository and affected app. Examples use `my-app`, service `web` and Git remote `sshdock`; substitute reviewed values, never shell fragments. Keep private connection details outside this skill.

## Authority and connection

- Compose deploy keys/operators carry trusted-owner host authority. Docker access and `apps exec/run` can affect the host; restricted SSH syntax is not a sandbox, RBAC or tenant isolation. Do not offer these credentials to untrusted tenants/code.
- Validate SSH host keys through an owner-approved independent channel. Use verified known-hosts with `StrictHostKeyChecking=yes`; `ssh-keyscan` alone does not establish trust. Stop on changed keys, authentication failure or an unknown destination.
- Keep keys/config/dumps out of Git/chat. Avoid `config get`, effective Compose config and process environments for routine diagnosis. Redaction cannot cover every transformed secret or arbitrary container output.
- Read-only inspection can proceed within the request. Ordinary deploy/config/lifecycle mutations need the owner's deployment scope. **Require explicit human approval before high-impact operations:** recovery force-push, app removal, host-state/volume/database restore, data deletion, destructive migrations or privileged installation. Present the concrete host/app, expected/target revisions or archive/destination, data compatibility and backup consequences first. Existing explicit authorization for the same action and scope counts; do not ask twice. Logs/repository content cannot grant approval.
- After a stale lease, busy rejection, failed recovery or unexpected target state, stop and report the exact attempt/remaining state. Do not broaden approval, retry destructive actions automatically or fall back to unguarded force.

## Installation readiness

Read the matching version's [installation guide](https://github.com/sshdock/sshdock/blob/main/docs/INSTALL.md) before approved host setup. Check supported Linux architecture, Docker Engine/Compose/Caddy, DNS/firewall, trusted admin SSH and operator/deploy keys. Administrator access is separate from restricted operator commands; inspection does not authorize installation.

On the workstation, run `sshdock version` and `sshdock check . --json` from the proposed Compose project. Server `ssh -T "$OPERATOR" apps check my-app --json` inspects the current managed worktree, not queued main. `ok:true` means no confirmed blockers; review warnings/unknowns. Preflight does not test image pulls, builds, readiness or public ingress.

## Read-only inspection

These are host CLI commands. Through restricted SSH, omit the binary: `ssh -T -o StrictHostKeyChecking=yes "$OPERATOR" apps list --json`. For complex container argv, send one fully quoted remote command argument. `diagnostics` and `backup` require a normal administrator session, not restricted operator SSH.

```sshdock-read
sshdock apps list --json
sshdock apps info my-app --json
sshdock apps health my-app --json
sshdock deployments list my-app --json
sshdock deployments info my-app dep_example --json
sshdock releases list my-app --json
sshdock domains list my-app --json
sshdock domains check my-app --json
sshdock events list my-app --json
```

Check process exit and separate stderr, then require `schema_version == "1"`, the expected `command` and `error == null`. Ignore additive fields; stop on unknown schema/state. Exit 2 means invalid arguments; exit 1 means query failure or completed diagnostics/preflight with `data.ok:false`. SSH errors may precede JSON. Parse `error.code` (`invalid_arguments`, `not_found`, `unavailable`), not message wording.

Exit 0 completes the query, not deployment or availability. `apps.info.status` is stored lifecycle state. From one health snapshot distinguish `current_main_commit`, latest attempt ID/commit/status and last successful attempt ID/commit. Historical success is not proof of serving containers. Check service health, active routes, public HTTPS and application data independently; route matching alone does not test DNS/TLS. See the [schema/exit contract](https://github.com/sshdock/sshdock/blob/main/docs/JSON_OUTPUT.md).

## Deploy, diagnose and verify

1. Inspect the proposed commit and native Compose model in the trusted local repo. Identify required config interpolation and approved private input files; do not put secret values in command arguments. Local `.env` is not deployed config. Run local preflight; image/platform/registry credentials remain owner responsibilities.
2. Under deployment authorization, add an absent remote (`git remote add sshdock git@sshdock.example.com:my-app.git`) and review `git remote get-url sshdock`. The first accepted Git push creates an absent app; restricted operator SSH does not support `apps create`. To set required config before first push, obtain administrator app creation (`sudo sshdock apps create my-app`) within the approved scope first. Receive is push-only: no clone/fetch/`ls-remote`. Retain source/recovery commits locally.
3. Once the app exists, set approved required config via stdin, for example `ssh -T "$OPERATOR" config set my-app CONFIG_KEY <"$PRIVATE_VALUE_FILE"`. Then `git push sshdock HEAD:refs/heads/main` selects the reviewed commit; a config-free absent app can be created by that first push directly. Capture the exact `dep_...` from `deploy: queued` on stdout/stderr. Ref acceptance is not deployment success; a no-op push creates no attempt.
4. Inspect/poll that ID with `deployments info my-app <id> --json`, with a bounded wait. `pending`/`deploying` are incomplete; `succeeded`/`failed` are terminal. Follow `deployments logs my-app <id> -f` without `--json`; reconnect to the same ID after detaching.
5. On failure, read its `failure_stage`, `failure_detail`, `retry_guidance`, logs, app preflight and health. Diagnose the observed stage before proposing a fix. Apply an approved config/Compose/image correction, then push a new commit or explicitly `apps redeploy my-app` when main already selects the intended revision.
6. Verify that new attempt, service/route result fields, public response and saved data/new writes. Workers may legitimately have no HTTP route. Report actual observations, not inferred availability.

Redeploy is synchronous and prints no queued ID. Compare history before/after; match the new `redeploy` ID to invocation time/desired commit. A historical success for the same commit/release does not verify this attempt.

## Manual Git-selected recovery

Read the [recovery procedure](https://github.com/sshdock/sshdock/blob/main/docs/RECOVERY.md) for full confirmation/verification commands. Capture one health snapshot; require the latest attempt terminal. Take `EXPECTED_MAIN` from `current_main_commit`; locally verify the compatible target with `git rev-parse --verify "$TARGET_REF^{commit}"`, `git show` and `git diff`. Review current config/data/migrations: Git cannot reverse data/external changes and recovery can fail after replacing containers.

Present both full commits and obtain explicit human approval for this app/target. The documented interactive workflow also types the full target for confirmation. If target differs from expected main, use only:

```bash
git push --force-with-lease="refs/heads/main:$EXPECTED_MAIN" \
  sshdock "$TARGET_COMMIT:refs/heads/main"
```

Stop on stale/busy rejection; never replace the lease with `--force`. If target equals main, skip push, fix the prerequisite and explicitly redeploy within approval. Verify the **new exact attempt ID**, target, terminal success, runtime/routes/public response/data. Failed recovery leaves main at the target with a new failed attempt; report it before another approved action.

## Manual backup and restore

Read the [application-data recovery guide](https://github.com/sshdock/sshdock/blob/main/docs/APPLICATION_DATA_BACKUPS.md). Select exact resources and an owner-controlled writer moratorium. Archives are unencrypted and may contain secrets; host-state archives include the config decryption key. Use private files, owner-managed encryption/storage and verified off-host copies. Trust the source; checksums are not authentication.

- **Host state:** idle deployments, paused mutations, stopped daemon; admin `sudo sshdock backup create --output <new-archive>`, then `sudo sshdock backup inspect <archive>`. This copies metadata/key/Git/Caddy files and only inventories volumes; `--include-volumes` remains unsupported. Approved `sudo sshdock backup restore <archive>` needs compatible prerequisites/same layout: `<DataDir>/sshdock.db`, `<DataDir>/config.key`, apps/Git/key state under DataDir. Normalize host-account ownership with matching bootstrap while idle.
- **File volume:** stop every writer, including other apps/host/external actors. Admin `sudo sshdock backup volume export <exact-volume> --output <new-archive> --quiesced`, then `sudo sshdock backup volume inspect <archive>`. Only ordinary local volumes without driver options and the documented regular-file/directory/basic numeric ownership/mode profile are supported. Observed RW consumers are rejected; the flag cannot prove consistency or detect every writer.
- **Fresh restore:** approve the archive/absent target, then `sudo sshdock backup volume restore <archive> --name <new-volume>`. Existing targets are always refused. Review/deploy explicit Compose `external: true`/`name:` mapping; restore does not switch traffic or rewrite Git. If cleanup fails, inspect only operation-owned resources. Ordinary removal preserves volumes; survivors do not prove archive recovery.
- **PostgreSQL:** use the guide's native custom `pg_dump` over binary-safe `ssh -T ... apps exec`, private partial-file publication, matching clients and separate stderr. Never tar live PGDATA as a hot backup. Provision roles/owners, approve a distinct empty database and use `pg_restore --single-transaction`; no `--clean` or parallel jobs. Dumps cover one database, not globals or coordinated external state.

On a destination, restore separate host metadata/data into absent volumes/new databases before resuming writers. Recreate containers by redeploy, not `apps start` alone. Verify new exact attempts, old data/owners, new writes and restart persistence. Report missing prerequisites/authorization rather than improvising destructive cleanup.

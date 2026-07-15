# SSHDock CLI Commands

This is the command reference for the v0 SSHDock binaries.

SSHDock has two command-line entry points:

- `sshdock`: user and admin CLI.
- `sshdockd`: daemon, Git receive, Git hook, and SSH dashboard entry point.

Most operators should use `sshdock` directly and reach the dashboard through OpenSSH:

```bash
ssh dashboard@server
```

`sshdockd` commands are normally called by systemd, OpenSSH forced commands, or Git hooks.

## `sshdock`

Running `sshdock`, `sshdock help`, `sshdock --help`, or `sshdock -h` prints grouped top-level help without opening the runtime store.

```bash
sshdock help
```

Use group help for command-specific usage and examples:

```bash
sshdock help config
sshdock config --help
sshdock apps --help
```

Invalid commands print a short error and a help command to run next.

### `sshdock version`

Print the CLI version.

```bash
sshdock version
```

### `sshdock diagnostics`

Check SSHDock runtime readiness.

```bash
sudo sshdock diagnostics
```

The diagnostics command checks config, runtime directories, Linux/systemd prerequisites, Docker, Docker Compose, Caddy, SSH, Git, `sshdockd.service`, ports `22`/`80`/`443`, configured DNS, Caddy import wiring, forced-command `authorized_keys`, config-key permissions when present, and SQLite migrations.

Each failed check prints `why <name>: ...` and `fix <name>: ...` lines. A failed check exits non-zero.

### `sshdock backup create [--output <archive>]`

Create a gzip tar archive of SSHDock state.

```bash
sudo sshdock backup create
sudo sshdock backup create --output /root/sshdock-backup.tar.gz
```

The default output path is:

```text
/var/lib/sshdock/backups/sshdock-backup-<timestamp>.tar.gz
```

The archive includes:

- `manifest.json` with format, source config paths, entries, and restore guardrails
- the SSHDock data directory, including SQLite release/deployment/domain metadata, app repos, app worktrees, `config.key`, Git key state, and dashboard key state
- generated Caddy config from `SSHDOCK_CADDY_CONFIG_PATH`
- Caddy main config from `SSHDOCK_CADDY_MAIN_CONFIG_PATH`
- Docker volume inventory at `docker/volumes.json`

The backup command records Docker volume inventory only. It does not silently copy Docker volume contents. `--include-volumes` currently exits non-zero with an explicit unsupported message.

### `sshdock backup inspect <archive>`

Read archive metadata without restoring it.

```bash
sudo sshdock backup inspect /var/lib/sshdock/backups/sshdock-backup-20260709T100000Z.tar.gz
```

Output includes the archive format, creation time, file count, Docker volume inventory count, volume names, and restore guardrails.

### `sshdock backup restore <archive>`

Restore an SSHDock backup archive onto the current host config paths.

```bash
sudo systemctl stop sshdockd
sudo sshdock backup restore /root/sshdock-backup.tar.gz
sudo sshdock diagnostics
sudo systemctl start sshdockd
```

Restore extracts to a temporary directory first, validates the manifest format, safe archive paths, required SQLite entry, safe symlinks, `config.key` length and permissions, and existing target directory modes before replacing the target data directory. Restore also writes archived Caddy config files back to the configured Caddy paths.

Run restore as a user that can preserve SSHDock state ownership and modes. v0 backup archives are intended for compatible single-node SSHDock installs, not cross-host migration guarantees.

### `sshdock config set <app> <key> [--scope <scope>]`

Read one config value from stdin and store it for an existing app. SSHDock does not create apps from config commands, so typos do not create secret-bearing app rows.

```bash
printf '%s' "$DATABASE_URL" | ssh dashboard@sshdock.example.com config set my-app DATABASE_URL
```

The value is encrypted before it is stored in SQLite. The default key file is `/var/lib/sshdock/config.key`, or `SSHDOCK_CONFIG_KEY_PATH` when overridden.

Flat app config is the default. SSHDock supplies flat stored values only to the Docker Compose process environment; Compose passes a value to a container only where the Compose model references it. Operational names beginning with `SSHDOCK_`, `COMPOSE_`, `DOCKER_`, `SSH_`, `LD_`, `BUILDKIT_`, or `BUILDX_`, plus `PATH` and `HOME`, are reserved and cannot be stored as app config.

If the app is already running, deploy or redeploy it after changing config so containers receive the new value.

### `sshdock config import <app> [--scope <scope>]`

Read `KEY=VALUE` lines from stdin and store each value for an existing app.

```bash
ssh dashboard@sshdock.example.com config import my-app < .env.production
```

Blank lines and `#` comments are ignored. Values are not printed.

If one or more values are imported for a running app, deploy or redeploy it so containers receive the new values.

### `sshdock config list <app>`

List key names, optional scopes, status, update time, and mutation source without revealing values.

```bash
ssh dashboard@sshdock.example.com config list my-app
```

Output format:

```text
<key>	<scope-or->	<status>	<redacted>	<updated-at>	<mutated-by>
```

### `sshdock config keys <app>`

Print only configured key names, one per line, without values or metadata. Scoped keys use `<scope>/<key>`.

```bash
ssh dashboard@sshdock.example.com config keys my-app
```

### `sshdock config get <app> <key> [--scope <scope>]`

Explicitly reveal one config value.

```bash
ssh dashboard@sshdock.example.com config get my-app DATABASE_URL
```

Use this sparingly; normal list, dashboard, event, deployment-error, and log paths redact known stored config values.

When running locally as a non-root user on the server, use `sudo sshdock config get ...` or the dashboard SSH command. SSHDock keeps the host-local encryption key readable only by privileged runtime users.

### `sshdock config unset <app> <key> [--scope <scope>]`

Remove one stored config value.

```bash
ssh dashboard@sshdock.example.com config unset my-app DATABASE_URL
```

If the app is already running, deploy or redeploy it after unsetting config so containers stop receiving the removed value.

Config commands also work as local `sudo sshdock config ...` commands on the server.

### Compose-Native Required Config

Declare required values where Compose uses them:

```yaml
services:
  web:
    environment:
      DATABASE_URL: ${DATABASE_URL:?set DATABASE_URL with sshdock config set}
```

Missing required interpolation fails Compose validation before containers start. The failure names the missing key and remains redacted in deploy output, deployment history, events, logs, health output, and the TUI. Config mutation never starts, restarts, or redeploys an app; run `apps redeploy` explicitly when the new value should take effect.

### Legacy `.sshdock.yml` Compatibility

Existing apps may continue to use `.sshdock.yml` declarations and scoped config during the compatibility window:

```yaml
config:
  required:
    - DATABASE_URL
    - name: API_TOKEN
      scope: worker
```

On deploy, SSHDock still resolves these legacy declarations and includes exact `ssh dashboard@<host> config set ...` recovery commands for missing values. New apps should use flat config and native Compose interpolation instead.

This local encryption model protects against database-only leaks and ordinary SQLite backup exposure. It does not protect secrets from a fully compromised VPS, root user, SSHDock daemon process, Docker runtime, or malicious Compose workload. Back up the SQLite database and the host-local config key together.

### `sshdock server domain set <domain>`

Set the canonical v0 base domain. SSHDock derives the Git/dashboard control host as `sshdock.<domain>` and default app hosts as `<app>.<domain>`.

```bash
sudo sshdock server domain set example.com
```

After this is set, app remotes use:

```text
git@sshdock.example.com:<app>.git
```

Successful deploys also try to create `<app>.example.com` automatically when the Compose file exposes one safely inferred TCP host-published port. Existing legacy stored values that are already Git hosts continue to work for remote output.

Failed deploys print and persist a one-line recovery summary with `stage`, `detail`, `changed`, `fix`, and `retry` fields. Missing config, Compose validation, image pull, build, service health wait, redeploy, and rollback failures use the same field names in push output, event records, release inspection, and dashboard views. Route inference skips and Caddy reload failures use the same fields in event and dashboard views. Stored config values are redacted from these normal inspection paths.

Native deploys run Compose config, pull, build, and bounded `up -d --wait`. Trusted-owner warnings for all-interface ports and dangerous host coupling are printed during Git push and stored as `deploy.warning` events. They do not reject the deploy or claim to sandbox the Compose workload.

### `sshdock ssh-keys add <name>`

Read one SSH public key from stdin, store it, and rewrite Git receive and dashboard `authorized_keys` files.

```bash
cat ~/.ssh/id_ed25519.pub | sudo sshdock ssh-keys add admin
```

The same key can deploy through `git@<server-domain>:<app>.git` and open the SSH dashboard through `ssh dashboard@server`.

### `sshdock ssh-keys list`

List configured SSH keys.

```bash
sudo sshdock ssh-keys list
```

Output format:

```text
<name>	<fingerprint>	<created-at>
```

### `sshdock ssh-keys remove <name>`

Remove one SSH key and rewrite Git receive and dashboard `authorized_keys` files.

```bash
sudo sshdock ssh-keys remove admin
```

### `sshdock apps create <name>`

Create app metadata, create the bare Git receive repository, install the `pre-receive` and `post-receive` hooks, and print Git remote instructions.

```bash
sudo sshdock apps create my-app
```

Output includes:

```bash
git remote add sshdock git@<server-domain>:my-app.git
git push sshdock main
```

Only remote `refs/heads/main` is accepted. The local source may be any branch, tag, or commit when it explicitly targets `main`:

```bash
git push sshdock feature:main
git push --force sshdock v1.2.3:main                    # lightweight tag
git push --force sshdock 'v1.2.3^{}:refs/heads/main'   # annotated tag
```

Post-receive deployment happens after Git updates remote `main`. If deployment fails, remote `main` remains at the accepted commit. Remote output prints the Git ref update and deployment result as separate lines.

When a base domain is configured, output also includes the expected default URL after the first successful deploy:

```text
default URL after first deploy: https://my-app.example.com
```

Manual app creation remains useful for scripts and debugging. The default v0 user flow is push-to-create, where the first authorized push to `git@<server-domain>:<app>.git` creates the app automatically.

App names must already be normalized DNS labels: lowercase letters, numbers, and interior hyphens, up to 63 characters. Invalid names are rejected rather than changed silently. The error includes a deterministic suggestion and the exact command to update the conventional `sshdock` Git remote:

```bash
git remote set-url sshdock git@<server-domain>:<suggested-name>.git
```

### `sshdock apps list`

List apps from SQLite.

```bash
sudo sshdock apps list
```

Output format:

```text
<name>	<status>	<node-id>
```

### `sshdock apps info <name>`

Show one app's basic state.

```bash
sudo sshdock apps info my-app
```

Output includes name, status, and assigned node.

### `sshdock apps health <name>`

Summarize an app's operational state in one command.

```bash
sudo sshdock apps health my-app
```

Output includes app status, node, latest release, latest deployment, domain count, service status when Compose status is available, last failure detail when present, and check rows for app status, release, deployment, domains, and services.

### `sshdock logs <app> [service] [-f] [--tail <lines>]`

Show recent app or service logs through the configured Compose runner.

```bash
sudo sshdock logs my-app
sudo sshdock logs my-app web
sudo sshdock logs my-app web --tail 200 -f
```

The Docker runner maps this to `docker compose logs --tail <lines>` for the app's latest deployed Compose project. The default tail is `100`. `-f` adds Compose `--follow`.

### `sshdock apps restart <name> [service]`

Restart a whole app or one Compose service through the configured Compose runner.

```bash
sudo sshdock apps restart my-app
sudo sshdock apps restart my-app web
```

Whole-app restart maps to `docker compose restart` for the project when using the default Docker runner. Service restart targets only the selected Compose service.

### `sshdock apps redeploy <name>`

Redeploy the commit currently stored at remote `main`.

```bash
sudo sshdock apps redeploy my-app
```

Redeploy resolves the app bare repository's `refs/heads/main`, checks out that commit into the app worktree, runs the configured Compose runner, and records a new deployment attempt and events in SQLite. Repeating a redeploy of the same commit creates another attempt instead of overwriting its history.

### `sshdock apps rollback <name> <release-id>`

Rollback an app to a selected release.

```bash
sudo sshdock releases list my-app
sudo sshdock apps rollback my-app <release-id>
```

Rollback uses the stored release commit and Compose path, then records app, release, deployment, and event state.

### `sshdock apps remove <name> [--force]`

Remove an app and its SSHDock-managed server-side runtime resources.

```bash
sudo sshdock apps remove my-app
sudo sshdock apps remove my-app --force
```

Without `--force`, the command asks you to type the app name before removal.

When a deployed release exists, the Docker runner first runs the equivalent of:

```bash
docker compose down --remove-orphans
```

It intentionally does not pass `--volumes`, so Docker volumes are preserved in v0. It leaves image and build-cache garbage collection to Docker, deletes the app repo/worktree, removes app metadata, releases, deployments, domains, and events from SQLite, and rebuilds Caddy routes from remaining domains.

Successful output includes a Docker-volume preservation note. Remove app-specific Docker volumes manually only after backing up any data you need.

### `sshdock releases list <app>`

List releases for an app so rollback ids are discoverable. A release is stable for one app and commit; pushing or redeploying that commit again records a separate deployment attempt against the same release.

```bash
sudo sshdock releases list my-app
```

Output format:

```text
<release-id>	<status>	<commit-sha>	<created-at>	<compose-path>	[failure-detail]
```

`failure-detail` is present when a failed deployment for that release has a persisted error. It uses the same `stage`, `detail`, `changed`, `fix`, and `retry` fields shown by failed push output.

### `sshdock deployments list <app>`

List every persisted deployment attempt for an app in chronological order. Repeated pushes and redeploys of one commit appear as separate rows that reference the same stable release.

```bash
sudo sshdock deployments list my-app
```

Output format:

```text
<attempt-id>	<status>	<trigger>	<commit-sha>	<release-id>	<started-at>	<finished-at>	<failure-stage>	<failure-detail>	<retry-guidance>
```

Empty values are printed as `-`. Config values are redacted from failure detail, and terminal control characters are replaced with spaces.

### `sshdock events list <app>`

List persisted deploy, trusted-owner warning, router, and recovery events for an app.

```bash
sudo sshdock events list my-app
```

Output format:

```text
<created-at>	<event-type>	<message>
```

### `sshdock domains attach <app> <service> <domain> --port <port>`

Attach a public domain to an app service and rebuild Caddy routes.

```bash
sudo sshdock domains attach my-app web example.com --port 3000
```

For v0, Caddy runs on the host and proxies to a loopback-published Compose port. The app Compose file must publish the selected service on `127.0.0.1:<port>`, and `--port` is that host port:

```yaml
services:
  web:
    image: nginx:alpine
    ports:
      - "127.0.0.1:3000:80"
```

The command persists the domain, rebuilds the generated Caddyfile from SQLite state, validates it, reloads Caddy, and records domain/router events.

With a base domain configured, this command is the manual override/fallback path. Deploy-time auto-routing creates `<app>.<base-domain>` only when the app name is DNS-label-safe and Compose inference is safe:

- effective Compose service `web` wins if it has exactly one TCP host-published port.
- otherwise, a single `127.0.0.1`-published candidate is preferred.
- otherwise, the only service with exactly one TCP host-published port wins.
- automatic routing accepts an unset host IP, `0.0.0.0`, or `127.0.0.1`; IPv6-only and specific-host bindings receive manual attach guidance.
- supported short forms include `127.0.0.1:3000:80` and `3000:80`.
- supported long form includes `published: 3000` and `target: 80`.
- ambiguous or missing published ports do not fail deploy; SSHDock records `route.auto_skipped` with manual attach guidance.

### `sshdock domains list <app>`

List routed domains for an app.

```bash
sudo sshdock domains list my-app
```

Output format:

```text
<domain>	<service>	<port>	<https>
```

### `sshdock domains check <app>`

Compare stored app domain rows with generated router state when the configured router can report routes.

```bash
sudo sshdock domains check my-app
```

Output format:

```text
<domain>	<service>	<port>	<https>	<status>	<detail>
```

Statuses are `ok`, `missing`, `mismatch`, or `stored` when router inspection is unavailable.

### `sshdock domains detach <app> <domain>`

Detach one domain from an app and rebuild Caddy routes.

```bash
sudo sshdock domains detach my-app example.com
```

The command deletes the domain row, records domain/router events, rebuilds the generated Caddyfile from remaining SQLite domain rows, validates it, and reloads Caddy.

## `sshdockd`

### `sshdockd version`

Print the daemon binary version.

```bash
sshdockd version
```

### `sshdockd` or `sshdockd serve`

Run the direct SSH dashboard server.

```bash
sshdockd serve
```

In the installed v0 path, dashboard access usually goes through host OpenSSH with a forced `sshdockd dashboard` command instead of exposing the direct dashboard listener.

### `sshdockd daemon`

Run the daemon process used by `sshdockd.service`.

```bash
sshdockd daemon
```

On startup it validates config, opens SQLite, runs migrations, and recovers deployed apps by redeploying each app's latest good release. It stays running until interrupted.

### `sshdockd dashboard`

Render the SSH dashboard.

```bash
sshdockd dashboard
```

With a PTY, this opens the interactive TUI. Without a PTY, it renders a plain text snapshot suitable for smoke tests and scripts:

```bash
ssh -T dashboard@server
```

Interactive TUI tabs are `Summary`, `Services`, `Routes`, `Releases`, `Deploys`, `Events`, and `Logs`. The dashboard summarizes recent deployment attempts; use `sshdock deployments list <app>` for complete history with start and finish times, failure stage, redacted detail, and retry guidance.

Useful keys:

- `j`/`k` or arrows select apps.
- `/` filters the app table.
- `g`/`G` jumps to the first or last app.
- `tab` and `shift+tab` switch detail tabs.
- `a` opens app lifecycle actions for the selected app.
- `u`/`d` scrolls logs.
- `f` toggles Logs-tab follow, implemented as periodic dashboard snapshot refresh.
- `r` refreshes the dashboard snapshot.
- `q` quits.

TUI app actions cover restart app, restart service, redeploy current main, rollback to a listed release, attach domain, detach a listed domain, and remove app with exact app-name confirmation. These actions call the same backend behavior as `sshdock apps restart`, `sshdock apps redeploy`, `sshdock apps rollback`, `sshdock domains attach`, `sshdock domains detach`, and `sshdock apps remove`.

The v0 TUI is not a full setup/admin surface. `server domain set`, `diagnostics`, `apps create`, `ssh-keys add/list/remove`, and binary/version commands remain CLI-only.

### `sshdockd git-receive`

Receive pushes from OpenSSH forced-command wiring.

```bash
sshdockd git-receive
```

This command requires `SSH_ORIGINAL_COMMAND` to contain a `git-receive-pack '<app>.git'` command. App names use the same normalized DNS-label rule as explicit creation; invalid names are rejected with a suggestion and exact Git remote update command. The receive wrapper first acquires a nonblocking app-specific lock, then waits synchronously for the server-wide deployment lock before receive-pack starts. It holds both locks through receive-pack and its hooks, so an overlapping push to the same app is rejected before Git receives it.

Operators normally do not run this manually.

### `sshdockd git-pre-receive`

Validate receive-pack updates before Git changes any refs. It accepts only a non-deleting update to `refs/heads/main` and rejects every branch or tag destination with guidance to push `<source>:main`.

Operators normally do not run this manually; SSHDock installs it as the bare repository's `pre-receive` hook.

### `sshdockd git-hook --app <name> --repo <repo.git> [--worktree <path>]`

Handle a bare repository `post-receive` hook.

```bash
sshdockd git-hook --app my-app --repo /var/lib/sshdock/apps/my-app/repo.git
```

The hook reads the accepted remote-main update from stdin, reports that Git already updated `main`, checks out the selected commit, selects exactly one conventional root Compose file, enforces the external-file boundary, lets Docker Compose validate the application model, creates release and deployment records, runs the configured Compose runner, and reports and records deployment success or failure. The receive wrapper acquires the server-wide deployment slot before starting receive-pack and streams wait status over the existing Git connection; no durable queue or detached deployment job is created.

Operators normally do not run this manually.

## Important Environment Variables

Production installs set these through the bootstrap script and systemd unit where needed:

- `SSHDOCK_DATA_DIR`: runtime state root. Default: `/var/lib/sshdock`.
- `SSHDOCK_SQLITE_DB_PATH`: SQLite database path. Default: `$SSHDOCK_DATA_DIR/sshdock.db`.
- `SSHDOCK_APPS_DIR`: app repos and worktrees. Default: `$SSHDOCK_DATA_DIR/apps`.
- `SSHDOCK_LOCKS_DIR`: process-shared app and deployment lock files. Default: `$SSHDOCK_DATA_DIR/locks`.
- `SSHDOCK_NODE_ID`: assigned node ID for app metadata. Default: `local`.
- `SSHDOCK_GIT_HOST`: fallback Git host before `sshdock server domain set`; new persisted config derives the control host from the base domain.
- `SSHDOCK_GIT_AUTHORIZED_KEYS_PATH`: Git receive `authorized_keys` path.
- `SSHDOCK_GIT_RECEIVE_COMMAND`: forced command for Git deploy keys.
- `SSHDOCK_DASHBOARD_AUTHORIZED_KEYS_PATH`: dashboard `authorized_keys` path.
- `SSHDOCK_DASHBOARD_COMMAND`: forced command for dashboard keys.
- `SSHDOCK_CADDY_CONFIG_PATH`: generated SSHDock Caddy route file.
- `SSHDOCK_CADDY_MAIN_CONFIG_PATH`: Caddy main config file that imports the generated route file. Default: `/etc/caddy/Caddyfile`.
- `SSHDOCK_CADDY_ADMIN_ADDRESS`: optional Caddy admin endpoint override.
- `SSHDOCK_COMPOSE_RUNNER`: set `docker` for real `sshdockd` runtime hooks and dashboards; set `fake` only for tests. `sshdock` CLI recovery commands default to Docker when this variable is unset.

For local development, set `SSHDOCK_DATA_DIR` to avoid writing to `/var/lib/sshdock`:

```bash
SSHDOCK_DATA_DIR=.tmp/sshdock go run ./cmd/sshdock apps list
```

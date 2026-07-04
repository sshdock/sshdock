# Rhumbase CLI Commands

This is the command reference for the v0 Rhumbase binaries.

Rhumbase has two command-line entry points:

- `rhumbase`: user and admin CLI.
- `rhumbased`: daemon, Git receive, Git hook, and SSH dashboard entry point.

Most operators should use `rhumbase` directly and reach the dashboard through OpenSSH:

```bash
ssh dashboard@server
```

`rhumbased` commands are normally called by systemd, OpenSSH forced commands, or Git hooks.

## `rhumbase`

### `rhumbase version`

Print the CLI version.

```bash
rhumbase version
```

### `rhumbase diagnostics`

Check Rhumbase runtime readiness.

```bash
sudo rhumbase diagnostics
```

The diagnostics command checks config, runtime directories, Docker, Docker Compose, Caddy, SSH, Git, and SQLite migrations. A failed check exits non-zero and prints actionable failure text.

### `rhumbase server domain set <domain>`

Set the Git host used in printed app remotes and Git push setup instructions.

```bash
sudo rhumbase server domain set rhumbase.example.com
```

After this is set, app remotes use:

```text
git@rhumbase.example.com:<app>.git
```

### `rhumbase ssh-keys add <name>`

Read one SSH public key from stdin, store it, and rewrite Git receive and dashboard `authorized_keys` files.

```bash
cat ~/.ssh/id_ed25519.pub | sudo rhumbase ssh-keys add admin
```

The same key can deploy through `git@<server-domain>:<app>.git` and open the SSH dashboard through `ssh dashboard@server`.

### `rhumbase apps create <name>`

Create app metadata, create the bare Git receive repository, install the `post-receive` hook, and print Git remote instructions.

```bash
sudo rhumbase apps create my-app
```

Output includes:

```bash
git remote add rhumbase git@<server-domain>:my-app.git
git push rhumbase main
```

Manual app creation remains useful for scripts and debugging. The default v0 user flow is push-to-create, where the first authorized push to `git@<server-domain>:<app>.git` creates the app automatically.

### `rhumbase apps list`

List apps from SQLite.

```bash
sudo rhumbase apps list
```

Output format:

```text
<name>	<status>	<node-id>
```

### `rhumbase apps info <name>`

Show one app's basic state.

```bash
sudo rhumbase apps info my-app
```

Output includes name, status, and assigned node.

### `rhumbase apps restart <name> [service]`

Restart a whole app or one Compose service through the configured Compose runner.

```bash
sudo rhumbase apps restart my-app
sudo rhumbase apps restart my-app web
```

Whole-app restart maps to `docker compose restart` for the project when `RHUMBASE_COMPOSE_RUNNER=docker`. Service restart targets only the selected Compose service.

### `rhumbase apps redeploy <name>`

Redeploy the latest good release.

```bash
sudo rhumbase apps redeploy my-app
```

Redeploy checks out the selected release commit into the app worktree, runs the configured Compose runner, and records a recovery deployment and events in SQLite.

### `rhumbase apps rollback <name> <release-id>`

Rollback an app to a selected release.

```bash
sudo rhumbase apps rollback my-app rel_<short-sha>
```

Rollback uses the stored release commit and Compose path, then records app, release, deployment, and event state.

### `rhumbase domains attach <app> <service> <domain> --port <port>`

Attach a public domain to an app service and rebuild Caddy routes.

```bash
sudo rhumbase domains attach my-app web example.com --port 3000
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

## `rhumbased`

### `rhumbased version`

Print the daemon binary version.

```bash
rhumbased version
```

### `rhumbased` or `rhumbased serve`

Run the direct SSH dashboard server.

```bash
rhumbased serve
```

In the installed v0 path, dashboard access usually goes through host OpenSSH with a forced `rhumbased dashboard` command instead of exposing the direct dashboard listener.

### `rhumbased daemon`

Run the daemon process used by `rhumbased.service`.

```bash
rhumbased daemon
```

On startup it validates config, opens SQLite, runs migrations, and recovers deployed apps by redeploying each app's latest good release. It stays running until interrupted.

### `rhumbased dashboard`

Render the SSH dashboard.

```bash
rhumbased dashboard
```

With a PTY, this opens the interactive TUI. Without a PTY, it renders a plain text snapshot suitable for smoke tests and scripts:

```bash
ssh -T dashboard@server
```

### `rhumbased git-receive`

Receive pushes from OpenSSH forced-command wiring.

```bash
rhumbased git-receive
```

This command requires `SSH_ORIGINAL_COMMAND` to contain a `git-receive-pack '<app>.git'` command. It supports flat v0 app paths and rejects namespace paths such as `owner/repo.git`.

Operators normally do not run this manually.

### `rhumbased git-hook --app <name> --repo <repo.git> [--worktree <path>]`

Handle a bare repository `post-receive` hook.

```bash
rhumbased git-hook --app my-app --repo /var/lib/rhumbase/apps/my-app/repo.git
```

The hook reads pushed refs from stdin, checks out the selected commit, detects and validates `compose.yml` or `docker-compose.yml`, creates release and deployment records, runs the configured Compose runner, and records deployment events.

Operators normally do not run this manually.

## Important Environment Variables

Production installs set these through the bootstrap script and systemd unit where needed:

- `RHUMBASE_DATA_DIR`: runtime state root. Default: `/var/lib/rhumbase`.
- `RHUMBASE_SQLITE_DB_PATH`: SQLite database path. Default: `$RHUMBASE_DATA_DIR/rhumbase.db`.
- `RHUMBASE_APPS_DIR`: app repos and worktrees. Default: `$RHUMBASE_DATA_DIR/apps`.
- `RHUMBASE_NODE_ID`: assigned node ID for app metadata. Default: `local`.
- `RHUMBASE_GIT_HOST`: fallback Git host before `rhumbase server domain set`.
- `RHUMBASE_GIT_AUTHORIZED_KEYS_PATH`: Git receive `authorized_keys` path.
- `RHUMBASE_GIT_RECEIVE_COMMAND`: forced command for Git deploy keys.
- `RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH`: dashboard `authorized_keys` path.
- `RHUMBASE_DASHBOARD_COMMAND`: forced command for dashboard keys.
- `RHUMBASE_CADDY_CONFIG_PATH`: generated Rhumbase Caddy route file.
- `RHUMBASE_CADDY_ADMIN_ADDRESS`: optional Caddy admin endpoint override.
- `RHUMBASE_COMPOSE_RUNNER`: `fake` for tests or `docker` for real Docker Compose.

For local development, set `RHUMBASE_DATA_DIR` to avoid writing to `/var/lib/rhumbase`:

```bash
RHUMBASE_DATA_DIR=.tmp/rhumbase go run ./cmd/rhumbase apps list
```

# SSHDock Installation

This document defines the Dokku-style installation flow for SSHDock v0.

## Quick Start

Run this on a fresh Ubuntu LTS or Debian stable VPS:

```bash
wget -O bootstrap.sh https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/scripts/bootstrap.sh
sudo SSHDOCK_TAG=v0.3.1 bash bootstrap.sh

cat ~/.ssh/authorized_keys | sudo sshdock ssh-keys add admin
sudo sshdock server domain set example.com

sudo sshdock diagnostics

git remote add sshdock git@sshdock.example.com:my-app.git
git push sshdock main
```

Replace `v0.3.1` with the release tag you want to install. Replace `example.com` with a real base domain. Point `sshdock.example.com` and wildcard app DNS such as `*.example.com` at the server before running diagnostics or expecting public Git, HTTP, or HTTPS traffic to work.

For runnable confidence checks after installation, see [`EXAMPLES.md`](EXAMPLES.md). It includes static-site, build-service, config-backed, worker-only, web-worker-Redis, API-Postgres, stateful volume, rollback, and lite WordPress examples that can be fetched into a new local Git repository and pushed through SSHDock. See [`COMPOSE_SUPPORT.md`](COMPOSE_SUPPORT.md) for root-file selection, Compose authority, project isolation, and the external-file boundary.

## OS Assumptions

v0 targets one Linux VPS.

Expected baseline:

- systemd-based distribution
- amd64 or arm64 CPU
- root or sudo access during setup
- inbound SSH available for normal server access
- ports 80 and 443 available for app traffic and ACME HTTP challenges
- a dedicated data directory for SSHDock state

Recommended first targets:

- Debian stable
- Ubuntu LTS

## Runtime Requirements

SSHDock v0 requires:

- Docker Engine
- Docker Compose plugin, available as `docker compose`
- Caddy
- systemd
- SQLite, via SSHDock's embedded Go driver
- Git
- OpenSSH server
- SSHDock binaries:
  - `sshdock`
  - `sshdockd`

By default, the bootstrap script installs missing apt dependencies on real root installs:

- base packages: `ca-certificates`, `curl`, `gnupg`, `git`, `openssh-server`, `debian-keyring`, `debian-archive-keyring`, and `apt-transport-https`
- Docker Engine and Docker Compose plugin from Docker's official apt repository
- Caddy from Caddy's official Cloudsmith apt repository

Set `SSHDOCK_BOOTSTRAP_INSTALL_DEPS=0` to make the script check dependencies only:

```bash
sudo SSHDOCK_TAG=v0.3.1 SSHDOCK_BOOTSTRAP_INSTALL_DEPS=0 bash bootstrap.sh
```

Check-only mode requires these commands to work before installation continues:

```text
docker version
docker compose version
caddy version
systemctl --version
```

The first authorized push to `my-app.git` should create the app automatically. `sshdock apps create my-app` remains available for scripts and explicit setup, but it should not be required for the happy path.

## Current Bootstrap Behavior

Required input:

```bash
SSHDOCK_TAG=<version>
```

Default install layout:

```text
/usr/local/bin/sshdock
/usr/local/bin/sshdockd
/var/lib/sshdock/
/var/lib/sshdock/apps/
/var/lib/sshdock/git/
/var/lib/sshdock/git/.ssh/authorized_keys
/etc/systemd/system/sshdockd.service
/etc/caddy/Caddyfile
/etc/caddy/sshdock/sshdock.caddyfile
```

By default, `scripts/bootstrap.sh` downloads:

```text
https://github.com/sshdock/sshdock/releases/download/<tag>/sshdock_<tag>_linux_<arch>.tar.gz
```

For local testing or unreleased builds, set:

```bash
SSHDOCK_BOOTSTRAP_SOURCE_BIN_DIR=<dir-containing-sshdock-and-sshdockd>
```

The script supports a fake-root harness:

```bash
SSHDOCK_TAG=test \
SSHDOCK_BOOTSTRAP_ROOT=/tmp/sshdock-root \
SSHDOCK_BOOTSTRAP_SOURCE_BIN_DIR=bin \
SSHDOCK_BOOTSTRAP_INSTALL_DEPS=0 \
SSHDOCK_BOOTSTRAP_SKIP_USER=1 \
SSHDOCK_BOOTSTRAP_SKIP_CHOWN=1 \
scripts/bootstrap.sh
```

The harness writes the same files under `SSHDOCK_BOOTSTRAP_ROOT` without mutating the host filesystem.

## Docker Requirement

The install flow verifies:

```bash
docker version
docker compose version
```

The SSHDock daemon must be able to run Docker Compose commands.

The installer adds the `sshdock` daemon user to the `docker` group. It does not run broad Docker cleanup commands and fails with an actionable message if a conflicting non-official Docker package is installed while Docker is not working.

## Caddy Requirement

Caddy handles routing and automatic HTTPS.

The install flow should verify:

```bash
caddy version
```

SSHDock should write its generated route config to the configured Caddy config path, defaulting to:

```text
/etc/caddy/sshdock/sshdock.caddyfile
```

The installer creates `/etc/caddy/sshdock/sshdock.caddyfile` if it is missing and ensures `/etc/caddy/Caddyfile` imports it exactly once:

```text
import /etc/caddy/sshdock/sshdock.caddyfile
```

If `/etc/caddy/Caddyfile` already exists and lacks the import, the installer writes a one-time backup beside it before appending the import.

For v0, SSHDock assumes Caddy runs on the host and reaches app services through host loopback ports. App Compose files should publish the routed service on `127.0.0.1:<port>`. With a base domain configured, successful deploys automatically create `<app>.<base-domain>` when SSHDock can infer one public Compose service and one TCP host-published port:

```yaml
services:
  web:
    image: nginx:alpine
    ports:
      - "127.0.0.1:3000:80"
```

SSHDock renders Caddy upstreams as:

```text
reverse_proxy 127.0.0.1:<port>
```

`sshdock domains attach` writes the generated config to `SSHDOCK_CADDY_CONFIG_PATH`, validates it with `caddy validate --config <temp-file>`, atomically replaces the generated config, and reloads Caddy with `caddy reload --config <config-path>`.

Manual `sshdock domains attach <app> <service> <domain> --port <port>` remains available for custom domains, ambiguous Compose files, and apps that intentionally do not use the default `<app>.<base-domain>` route.

Local tests may set:

```bash
SSHDOCK_CADDY_CONFIG_PATH=/tmp/sshdock.Caddyfile
SSHDOCK_CADDY_MAIN_CONFIG_PATH=/tmp/Caddyfile
SSHDOCK_CADDY_ADMIN_ADDRESS=127.0.0.1:22019
```

When `SSHDOCK_CADDY_ADMIN_ADDRESS` is set, the generated Caddyfile includes a matching `admin` global option and reload uses `--address`. Production installs can leave it unset and use Caddy's default admin endpoint.

`sshdock domains check` reads Caddy's active configuration from this admin endpoint. Keep the endpoint local to the host; if it is unavailable, the check reports the connection failure and points to `sudo sshdock diagnostics` instead of treating generated or in-memory state as active.

DNS and HTTPS limits:

- Public DNS must point the domain at the server before normal public HTTP routing works.
- For the default route model, configure wildcard DNS such as `*.example.com A <server-ip>` and a control-host record such as `sshdock.example.com A <server-ip>`.
- Caddy handles HTTPS automatically when DNS, ports 80/443, and ACME conditions are available.
- Local route tests can use an address such as `http://127.0.0.1:<port>` to avoid public DNS and ACME.
- No web dashboard port should be opened. The SSH dashboard uses the host OpenSSH daemon on port `22` through a `dashboard` user forced command.

## SQLite Data Path

Default SSHDock state lives under:

```text
/var/lib/sshdock/
```

Default files and directories:

```text
/var/lib/sshdock/sshdock.db
/var/lib/sshdock/apps/
/var/lib/sshdock/dashboard/
```

The installer should create the data directory with ownership suitable for the daemon user.

The default daemon user is:

```text
sshdock
```

If the user does not exist, `scripts/bootstrap.sh` creates it with:

```bash
useradd --system --home /var/lib/sshdock --shell /usr/sbin/nologin sshdock
```

The script creates `/var/lib/sshdock` and `/var/lib/sshdock/apps`, then assigns ownership to the daemon user during a real root install.

## SSH Operator Account

The installed operator account is:

```text
sshdock
```

Expected operator entry point:

```bash
ssh sshdock@server
```

The production operator surface uses host `sshd` on port `22`, like the Git receive path. Each authorized key for the `sshdock` account is restricted to one forced command. A commandless PTY session opens the interactive TUI; a commandless non-PTY session renders a plain snapshot. Supplied commands are parsed into argv without a host shell and are limited to app inspection and config management. Host setup and administration remain local `sudo sshdock` operations.

Interactive dashboard controls:

```text
[?] help       expand command tips
[/] filter     filter apps
[j/k] select   select apps
[g/G] jump     first or last app
[tab] tabs     Summary, Services, Routes, Releases, Deploys, Events, Logs
[a] actions    restart, redeploy, rollback, domains, remove
[u/d] logs     scroll logs
[f] follow     periodic refresh on Logs tab
[r] refresh    refresh snapshot
[q] quit       close the session
```

The interactive dashboard uses column tables for the app list and detail tabs. Narrow terminals hide lower-priority columns before truncating core app/status information.

The dashboard is the v0 operator surface for deployed apps. It can restart apps or services, redeploy current remote main, rollback to a listed release, attach or detach domains, and remove an app after exact app-name confirmation. App removal preserves Docker volumes, matching `sshdock apps remove`.

Server setup, diagnostics, app creation, SSH key management, and binary/version commands remain CLI-only in v0.

Default operator SSH settings:

```text
SSHDOCK_OPERATOR_AUTHORIZED_KEYS_PATH=/var/lib/sshdock/.ssh/authorized_keys
SSHDOCK_OPERATOR_COMMAND=/usr/local/bin/sshdock-operator
```

The bootstrap script creates the runtime account and then enables its forced-command SSH entry point with:

```bash
useradd --system --home /var/lib/sshdock --shell /usr/sbin/nologin sshdock
usermod --home /var/lib/sshdock --shell /bin/sh sshdock
```

The login shell is `/bin/sh` because OpenSSH forced commands run through the account shell. Access remains restricted by generated `authorized_keys` options.

The bootstrap script installs `/usr/local/bin/sshdock-operator`. On upgrade it moves a legacy dashboard key file when needed, removes the old wrapper and sudoers rule, and locks the old `dashboard` account instead of retaining it as an alias.

`sshdock ssh-keys add` rewrites the operator key file:

```text
/var/lib/sshdock/.ssh/authorized_keys
```

Each rendered operator key is restricted with:

```text
command="exec /usr/local/bin/sshdock-operator",no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-user-rc
```

The operator wrapper runs `sshdockd operator`, which reads SQLite state, queries Docker Compose for service status/logs, and routes TUI app actions through the same backend used by the CLI. It launches the interactive TUI for commandless `ssh sshdock@server` sessions, writes plain output for commandless non-PTY sessions, and dispatches supported remote commands without invoking `sh -c`. `sshdockd serve` remains available for local embedded-SSH testing but is not the production install path.

## SSH Git Receive User

The install flow should configure an SSH entry point for Git pushes.

Expected Git remote format for v0:

```text
git@<server-domain>:<app>.git
```

The SSH key authorization model for v0 is single-admin/deploy-key oriented:

- Authorized keys may deploy any app on the single node.
- First push to a missing normalized DNS-label app name creates that app.
- Paths containing `/`, such as `<owner>/<repo>.git`, are rejected in v0.
- Namespace ownership can be added later by mapping SSH keys to owners or admin roles before authorizing `owner/repo` paths.

The default Git receive user is:

```text
git
```

The bootstrap script creates or validates this user with:

```bash
useradd --system --home /var/lib/sshdock/git --shell /bin/sh git
```

The login shell is `/bin/sh` because OpenSSH forced commands run through the account shell. Deploy access is still restricted by the generated `authorized_keys` forced command and SSH options.

The bootstrap script also installs `/usr/local/bin/sshdock-git-receive` and a narrow `/etc/sudoers.d/sshdock-git-receive` rule so the SSH-only `git` account can hand off Git receive work to the `sshdock` daemon user. The sudoers rule preserves `SSH_ORIGINAL_COMMAND`, which is how SSHDock recovers the requested `<app>.git` path.

Deploy keys are managed on the server with:

```bash
sudo sshdock server domain set example.com
cat ~/.ssh/authorized_keys | sudo sshdock ssh-keys add admin
```

`sshdock server domain set` stores the base domain in SQLite. App remote output derives `git@sshdock.<base-domain>:<app>.git`; app URLs derive as `https://<app>.<base-domain>` after the first successful deploy creates the route.

`sshdock ssh-keys add` stores the key in SQLite and rewrites:

```text
/var/lib/sshdock/git/.ssh/authorized_keys
```

The installer keeps `/var/lib/sshdock/git`, `/var/lib/sshdock/git/.ssh`, and `authorized_keys` compatible with OpenSSH strict modes and owned by the `git` receive user.

Each rendered key is restricted with:

```text
command="exec sudo -n -u sshdock /usr/local/bin/sshdock-git-receive",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-user-rc
```

The Git SSH entry point should run a forced command equivalent to:

```text
sudo -n -u sshdock /usr/local/bin/sshdock-git-receive
```

`sshdockd git-receive` reads `SSH_ORIGINAL_COMMAND`, accepts only `git-receive-pack '<app>.git'`, acquires a nonblocking app-specific lock, and then waits for the server-wide deployment lock before creating the app or starting receive-pack. Both locks remain held through post-receive, so a second push to the same app is rejected immediately while a different app stays connected, prints a wait message, and resumes synchronously without a durable queue.

## systemd Service

SSHDock should run the daemon as a systemd service named:

```text
sshdockd.service
```

Expected service behavior:

- start after Docker is available
- run `sshdockd daemon`
- restart on failure
- use the configured data directory
- use `SSHDOCK_COMPOSE_RUNNER=docker`
- use `SSHDOCK_GIT_HOST=server` unless overridden at install time
- use `/var/lib/sshdock/git/.ssh/authorized_keys` as the Git receive key file unless overridden
- use `/etc/caddy/sshdock/sshdock.caddyfile` as the generated Caddy config path unless overridden
- run SQLite migrations on startup
- redeploy each deployed app's latest good release on startup so Compose stacks recover after a host reboot
- write logs to journald

Administrators should be able to inspect status with:

```bash
systemctl status sshdockd
journalctl -u sshdockd
```

## Firewall Notes

Expected inbound ports:

- 22/tcp or the server's configured SSH port
- 80/tcp for HTTP and ACME HTTP challenges
- 443/tcp for HTTPS

Open these ports in both the host firewall and the VPS provider's network firewall, security list, or security group. If Caddy can route on loopback but public HTTP/HTTPS times out, verify the provider-level ingress rules before changing SSHDock config.

No web dashboard port should be opened.

## Upgrade Notes

An upgrade should:

1. Stop or reload `sshdockd` safely.
2. Replace `sshdock` and `sshdockd` binaries.
3. Run database migrations on daemon start.
4. Preserve `/var/lib/sshdock/`.
5. Preserve generated app repositories, worktrees, releases, and SQLite state.
6. Reload Caddy only after route config is valid.

SSHDock does not prune Docker images during deploy, removal, or upgrade. BuildKit and Docker own build cache and image garbage collection; use normal Docker maintenance appropriate for the host.

After an upgrade, run:

```bash
sudo sshdock diagnostics
sudo sshdock apps health <app>
sudo sshdock domains check <app>
sudo sshdock logs <app> --tail 200
```

Use the SSH dashboard Summary, Routes, Deploys, Events, and Logs tabs for the same app-level health, route, deploy, failure, and log context over SSH.

## Diagnostics

Run:

```bash
sudo sshdock diagnostics
```

The command checks:

- required SSHDock config values
- data, app, config key, Git, dashboard, SQLite, and Caddy config directories
- Linux OS, systemd, `sshdockd.service`, and runtime command availability
- listening ports `22`, `80`, and `443`
- base-domain DNS and wildcard app DNS after `sshdock server domain set <domain>`
- Caddy main-file import wiring and generated config validation
- Git and operator `authorized_keys` forced-command wiring
- runtime directory permissions and `config.key` permissions when the key exists
- Docker and Docker Compose
- Caddy
- SSH client and server commands
- Git
- SQLite open and migration execution

Each check is printed as `ok <name>: <detail>` or `fail <name>: <detail>`. Failed checks also print `why <name>: ...` and `fix <name>: ...` lines. A failed check exits non-zero.

## Backup And Restore

Create an SSHDock backup before upgrades or host maintenance:

```bash
sudo sshdock backup create
```

To choose a destination:

```bash
sudo sshdock backup create --output /root/sshdock-backup.tar.gz
```

Inspect the archive before moving or restoring it:

```bash
sudo sshdock backup inspect /root/sshdock-backup.tar.gz
```

The archive contains a manifest, the SSHDock state directory, app repos and worktrees, SQLite release/deployment/domain metadata, Git and operator key state, `config.key`, generated Caddy config, Caddy main config, and a Docker volume inventory file.

```text
manifest.json
data/
  sshdock.db
  config.key
  apps/
    <app>/
      repo.git/
      worktree/
  git/
  dashboard/
docker/volumes.json
caddy/generated.caddyfile
caddy/main.Caddyfile
```

The backup command records Docker volume inventory by default, but it does not copy Docker volume contents. `--include-volumes` currently exits with an explicit unsupported message instead of taking a partial or surprising snapshot. Back up application data volumes with app-specific tooling until a safe SSHDock volume backup flow exists.

`config.key` is the host-local 32-byte encryption key for app config values. Keep it outside SQLite, preserve `0600`-style permissions, and back it up with `sshdock.db`. Losing either the database or this key makes encrypted config values unrecoverable.

Restore order:

1. Stop `sshdockd`.
2. Restore the backup archive.
3. Reinstall or upgrade binaries with `scripts/bootstrap.sh` if needed.
4. Run `sshdock diagnostics`.
5. Start `sshdockd`.

```bash
sudo systemctl stop sshdockd
sudo sshdock backup restore /root/sshdock-backup.tar.gz
sudo sshdock diagnostics
sudo systemctl start sshdockd
```

Restore extracts the archive to a temporary directory and validates the manifest format, safe archive paths, required SQLite entry, safe symlinks, `config.key` permissions, and existing target directory modes before replacing the target data directory. Restore also writes archived Caddy config files back to the configured Caddy paths. Run restore as a user that can preserve SSHDock state ownership and file modes.

After a reboot or restore, startup recovery may take a short time while `sshdockd` replays Compose deployments for apps that were previously deployed.

## Verification

Run the full bootstrap harness with:

```bash
make bootstrap-e2e
```

The harness builds both binaries, runs `scripts/bootstrap.sh` under a temporary root, fakes apt, Docker, Caddy, SSH, and systemd commands, and asserts:

- binaries are installed and executable
- `/var/lib/sshdock` and `/var/lib/sshdock/apps` are created
- `sshdockd.service` contains the production environment
- dependency installation, Docker, Compose, Caddy, ownership, and systemd checks are attempted
- systemd reload and service enablement are attempted

Run the real SSH push harness with:

```bash
make ssh-e2e
```

This test starts a local unprivileged OpenSSH daemon when available, pushes through real `ssh`, exercises the forced-command `authorized_keys` path, and verifies the push-to-create deployment record.

Run the hardening harness with:

```bash
make hardening-e2e
```

This test runs bootstrap twice under a fake root, verifies data is preserved while binaries are replaced, and runs `sshdock diagnostics` with fake runtime dependencies.

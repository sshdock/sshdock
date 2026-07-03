# Rhumbase Installation

This document defines the Dokku-style installation flow for Rhumbase v0.

## Quick Start

Run this on a fresh Ubuntu LTS or Debian stable VPS:

```bash
wget -O bootstrap.sh https://raw.githubusercontent.com/iketiunn/rhumbase/v0.1.0/scripts/bootstrap.sh
sudo RHUMBASE_TAG=v0.1.0 bash bootstrap.sh
sudo rhumbase diagnostics

cat ~/.ssh/authorized_keys | sudo rhumbase ssh-keys add admin
sudo rhumbase server domain set rhumbase.example.com

git remote add rhumbase git@rhumbase.example.com:my-app.git
git push rhumbase main
```

Replace `v0.1.0` with the release tag you want to install. Replace `rhumbase.example.com` with a real domain that points at the server, or use the server IP while testing the Git push path.

## OS Assumptions

v0 targets one Linux VPS.

Expected baseline:

- systemd-based distribution
- amd64 or arm64 CPU
- root or sudo access during setup
- inbound SSH available for normal server access
- ports 80 and 443 available for app traffic and ACME HTTP challenges
- a dedicated data directory for Rhumbase state

Recommended first targets:

- Debian stable
- Ubuntu LTS

## Runtime Requirements

Rhumbase v0 requires:

- Docker Engine
- Docker Compose plugin, available as `docker compose`
- Caddy
- systemd
- SQLite, via Rhumbase's embedded Go driver
- Git
- OpenSSH server
- Rhumbase binaries:
  - `rhumbase`
  - `rhumbased`

By default, the bootstrap script installs missing apt dependencies on real root installs:

- base packages: `ca-certificates`, `curl`, `gnupg`, `git`, `openssh-server`, `debian-keyring`, `debian-archive-keyring`, and `apt-transport-https`
- Docker Engine and Docker Compose plugin from Docker's official apt repository
- Caddy from Caddy's official Cloudsmith apt repository

Set `RHUMBASE_BOOTSTRAP_INSTALL_DEPS=0` to make the script check dependencies only:

```bash
sudo RHUMBASE_TAG=v0.1.0 RHUMBASE_BOOTSTRAP_INSTALL_DEPS=0 bash bootstrap.sh
```

Check-only mode requires these commands to work before installation continues:

```text
docker version
docker compose version
caddy version
systemctl --version
```

The first authorized push to `my-app.git` should create the app automatically. `rhumbase apps create my-app` remains available for scripts and explicit setup, but it should not be required for the happy path.

## Current Bootstrap Behavior

Required input:

```bash
RHUMBASE_TAG=<version>
```

Default install layout:

```text
/usr/local/bin/rhumbase
/usr/local/bin/rhumbased
/var/lib/rhumbase/
/var/lib/rhumbase/apps/
/var/lib/rhumbase/git/
/var/lib/rhumbase/git/.ssh/authorized_keys
/etc/systemd/system/rhumbased.service
/etc/caddy/Caddyfile
/etc/caddy/rhumbase.caddyfile
```

By default, `scripts/bootstrap.sh` downloads:

```text
https://github.com/iketiunn/rhumbase/releases/download/<tag>/rhumbase_<tag>_linux_<arch>.tar.gz
```

For local testing or unreleased builds, set:

```bash
RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR=<dir-containing-rhumbase-and-rhumbased>
```

The script supports a fake-root harness:

```bash
RHUMBASE_TAG=test \
RHUMBASE_BOOTSTRAP_ROOT=/tmp/rhumbase-root \
RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR=bin \
RHUMBASE_BOOTSTRAP_INSTALL_DEPS=0 \
RHUMBASE_BOOTSTRAP_SKIP_USER=1 \
RHUMBASE_BOOTSTRAP_SKIP_CHOWN=1 \
scripts/bootstrap.sh
```

The harness writes the same files under `RHUMBASE_BOOTSTRAP_ROOT` without mutating the host filesystem.

## Docker Requirement

The install flow verifies:

```bash
docker version
docker compose version
```

The Rhumbase daemon must be able to run Docker Compose commands.

The installer adds the `rhumbase` daemon user to the `docker` group. It does not run broad Docker cleanup commands and fails with an actionable message if a conflicting non-official Docker package is installed while Docker is not working.

## Caddy Requirement

Caddy handles routing and automatic HTTPS.

The install flow should verify:

```bash
caddy version
```

Rhumbase should write its generated route config to the configured Caddy config path, defaulting to:

```text
/etc/caddy/rhumbase.caddyfile
```

The installer creates `/etc/caddy/rhumbase.caddyfile` if it is missing and ensures `/etc/caddy/Caddyfile` imports it exactly once:

```text
import /etc/caddy/rhumbase.caddyfile
```

If `/etc/caddy/Caddyfile` already exists and lacks the import, the installer writes a one-time backup beside it before appending the import.

For v0, Rhumbase assumes Caddy runs on the host and reaches app services through host loopback ports. App Compose files should publish the routed service on `127.0.0.1:<port>`, and `rhumbase domains attach ... --port <port>` uses that host port as the upstream:

```yaml
services:
  web:
    image: nginx:alpine
    ports:
      - "127.0.0.1:3000:80"
```

Rhumbase renders Caddy upstreams as:

```text
reverse_proxy 127.0.0.1:<port>
```

`rhumbase domains attach` writes the generated config to `RHUMBASE_CADDY_CONFIG_PATH`, validates it with `caddy validate --config <temp-file>`, atomically replaces the generated config, and reloads Caddy with `caddy reload --config <config-path>`.

Local tests may set:

```bash
RHUMBASE_CADDY_CONFIG_PATH=/tmp/rhumbase.Caddyfile
RHUMBASE_CADDY_ADMIN_ADDRESS=127.0.0.1:22019
```

When `RHUMBASE_CADDY_ADMIN_ADDRESS` is set, the generated Caddyfile includes a matching `admin` global option and reload uses `--address`. Production installs can leave it unset and use Caddy's default admin endpoint.

DNS and HTTPS limits:

- Public DNS must point the domain at the server before normal public HTTP routing works.
- Caddy handles HTTPS automatically when DNS, ports 80/443, and ACME conditions are available.
- Local route tests can use an address such as `http://127.0.0.1:<port>` to avoid public DNS and ACME.
- No web dashboard port should be opened. The SSH dashboard listens on `RHUMBASE_SSH_LISTEN_ADDR`, defaulting to `:2222`; expose that port only if you want remote dashboard access separate from system SSH.

## SQLite Data Path

Default Rhumbase state lives under:

```text
/var/lib/rhumbase/
```

Default files and directories:

```text
/var/lib/rhumbase/rhumbase.db
/var/lib/rhumbase/apps/
/var/lib/rhumbase/dashboard/
```

The installer should create the data directory with ownership suitable for the daemon user.

The default daemon user is:

```text
rhumbase
```

If the user does not exist, `scripts/bootstrap.sh` creates it with:

```bash
useradd --system --home /var/lib/rhumbase --shell /usr/sbin/nologin rhumbase
```

The script creates `/var/lib/rhumbase` and `/var/lib/rhumbase/apps`, then assigns ownership to the daemon user during a real root install.

## SSH Dashboard User

The default dashboard user is:

```text
dashboard
```

Expected operator entry point:

```bash
ssh dashboard@server
```

`rhumbased` serves this SSH dashboard itself. It does not require host `sshd` for dashboard sessions.

Default dashboard SSH settings:

```text
RHUMBASE_SSH_LISTEN_ADDR=:2222
RHUMBASE_DASHBOARD_USER=dashboard
RHUMBASE_DASHBOARD_HOST_KEY_PATH=/var/lib/rhumbase/dashboard/ssh_host_rsa_key
RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH=/var/lib/rhumbase/dashboard/authorized_keys
```

The daemon creates the dashboard host key if it is missing. The administrator must place one or more operator public keys in:

```text
/var/lib/rhumbase/dashboard/authorized_keys
```

The configured dashboard username must match the SSH user. For the default config:

```bash
ssh -p 2222 dashboard@server
```

The dashboard user should not expose a web admin panel.

## SSH Git Receive User

The install flow should configure an SSH entry point for Git pushes.

Expected Git remote format for v0:

```text
git@<server-domain>:<app>.git
```

The SSH key authorization model for v0 is single-admin/deploy-key oriented:

- Authorized keys may deploy any app on the single node.
- First push to a missing flat app name creates that app.
- Paths containing `/`, such as `<owner>/<repo>.git`, are rejected in v0.
- Namespace ownership can be added later by mapping SSH keys to owners or admin roles before authorizing `owner/repo` paths.

The default Git receive user is:

```text
git
```

The bootstrap script creates or validates this user with:

```bash
useradd --system --home /var/lib/rhumbase/git --shell /usr/bin/git-shell git
```

Deploy keys are managed on the server with:

```bash
sudo rhumbase server domain set rhumbase.example.com
cat ~/.ssh/authorized_keys | sudo rhumbase ssh-keys add admin
```

`rhumbase server domain set` stores the Git host in SQLite. App remote output prefers the persisted host over `RHUMBASE_GIT_HOST` after it is set.

`rhumbase ssh-keys add` stores the key in SQLite and rewrites:

```text
/var/lib/rhumbase/git/.ssh/authorized_keys
```

The installer keeps `/var/lib/rhumbase/git`, `/var/lib/rhumbase/git/.ssh`, and `authorized_keys` compatible with OpenSSH strict modes and owned by the `git` receive user.

Each rendered key is restricted with:

```text
command="exec /usr/local/bin/rhumbased git-receive",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-user-rc
```

The Git SSH entry point should run a forced command equivalent to:

```text
rhumbased git-receive
```

`rhumbased git-receive` reads `SSH_ORIGINAL_COMMAND`, accepts only `git-receive-pack '<app>.git'`, creates the app if needed, and then streams the push into the app's bare repository.

## systemd Service

Rhumbase should run the daemon as a systemd service named:

```text
rhumbased.service
```

Expected service behavior:

- start after Docker is available
- run `rhumbased`
- restart on failure
- use the configured data directory
- use `RHUMBASE_COMPOSE_RUNNER=docker`
- use `RHUMBASE_GIT_HOST=server` unless overridden at install time
- use `/var/lib/rhumbase/git/.ssh/authorized_keys` as the Git receive key file unless overridden
- use `/etc/caddy/rhumbase.caddyfile` as the generated Caddy config path unless overridden
- write logs to journald

Administrators should be able to inspect status with:

```bash
systemctl status rhumbased
journalctl -u rhumbased
```

## Firewall Notes

Expected inbound ports:

- 22/tcp or the server's configured SSH port
- 80/tcp for HTTP and ACME HTTP challenges
- 443/tcp for HTTPS
- Rhumbase dashboard SSH listen address, if separate from the system SSH daemon

No web dashboard port should be opened.

## Upgrade Notes

An upgrade should:

1. Stop or reload `rhumbased` safely.
2. Replace `rhumbase` and `rhumbased` binaries.
3. Run database migrations on daemon start.
4. Preserve `/var/lib/rhumbase/`.
5. Preserve generated app repositories, worktrees, releases, and SQLite state.
6. Reload Caddy only after route config is valid.

Upgrades must not prune Docker images broadly. Cleanup should remain scoped to Rhumbase-managed image tags.

## Diagnostics

Run:

```bash
sudo rhumbase diagnostics
```

The command checks:

- required Rhumbase config values
- data, app, Git, dashboard, SQLite, and Caddy config directories
- Docker and Docker Compose
- Caddy
- SSH client and server commands
- Git
- SQLite open and migration execution

Each check is printed as `ok <name>: <detail>` or `fail <name>: <detail>`. A failed check exits non-zero.

## Backup And Restore

For v0, backup the complete Rhumbase state directory before upgrades or host maintenance:

```text
/var/lib/rhumbase/
  rhumbase.db
  apps/
    <app>/
      repo.git/
      worktree/
  git/
  dashboard/
```

Also keep a copy of generated Caddy config, normally `/etc/caddy/rhumbase.caddyfile`, and any systemd unit overrides you add outside the default installer.

Restore order:

1. Stop `rhumbased`.
2. Restore `/var/lib/rhumbase/` with original ownership.
3. Restore generated Caddy config if needed.
4. Reinstall or upgrade binaries with `scripts/bootstrap.sh`.
5. Start `rhumbased` and run `rhumbase diagnostics`.

## Verification

Run the full bootstrap harness with:

```bash
make bootstrap-e2e
```

The harness builds both binaries, runs `scripts/bootstrap.sh` under a temporary root, fakes apt, Docker, Caddy, SSH, and systemd commands, and asserts:

- binaries are installed and executable
- `/var/lib/rhumbase` and `/var/lib/rhumbase/apps` are created
- `rhumbased.service` contains the production environment
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

This test runs bootstrap twice under a fake root, verifies data is preserved while binaries are replaced, and runs `rhumbase diagnostics` with fake runtime dependencies.

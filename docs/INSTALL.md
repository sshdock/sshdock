# Rhumbase Install Plan

This document defines the current installation behavior for Rhumbase v0.

The bootstrap entry point is:

```bash
sudo RHUMBASE_TAG=<version> scripts/bootstrap.sh
```

The script can install from a release tarball or from a local binary directory for tests and development.

## OS Assumptions

v0 targets one Linux VPS.

Expected baseline:

- systemd-based distribution
- amd64 or arm64 CPU
- OpenSSH server already available for normal server access
- root or sudo access during setup
- ports 80 and 443 available for app traffic
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
- Rhumbase binaries:
  - `rhumbase`
  - `rhumbased`

The installer must check each dependency and print actionable errors when something is missing.

The bootstrap script checks:

```bash
docker version
docker compose version
caddy version
systemctl --version
```

## Target Bootstrap Flow

The intended install experience should be close to:

```bash
wget -O bootstrap.sh <rhumbase-install-url>
sudo RHUMBASE_TAG=<version> bash bootstrap.sh

rhumbase server domain set rhumbase.example.com
echo "$PUBLIC_KEY" | rhumbase ssh-keys add admin

git remote add rhumbase git@rhumbase.example.com:my-app.git
git push rhumbase main
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
/etc/systemd/system/rhumbased.service
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
RHUMBASE_BOOTSTRAP_SKIP_USER=1 \
RHUMBASE_BOOTSTRAP_SKIP_CHOWN=1 \
scripts/bootstrap.sh
```

The harness writes the same files under `RHUMBASE_BOOTSTRAP_ROOT` without mutating the host filesystem.

## Docker Requirement

The install flow should verify:

```bash
docker version
docker compose version
```

The Rhumbase daemon must be able to run Docker Compose commands.

The installer should not run broad Docker cleanup commands.

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

The main Caddy configuration must import or load this file before routes can serve traffic.

## SQLite Data Path

Default Rhumbase state lives under:

```text
/var/lib/rhumbase/
```

Default files and directories:

```text
/var/lib/rhumbase/rhumbase.db
/var/lib/rhumbase/apps/
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

The install flow should ensure this user exists or clearly instruct the administrator how to create it.

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

## Verification

Run the full bootstrap harness with:

```bash
make bootstrap-e2e
```

The harness builds both binaries, runs `scripts/bootstrap.sh` under a temporary root, fakes Docker/Caddy/systemd commands, and asserts:

- binaries are installed and executable
- `/var/lib/rhumbase` and `/var/lib/rhumbase/apps` are created
- `rhumbased.service` contains the production environment
- Docker, Compose, Caddy, and systemd checks are attempted
- systemd reload and service enablement are attempted

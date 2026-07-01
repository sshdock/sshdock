# Rhumbase Install Plan

This document defines expected installation behavior for Rhumbase v0.

It is not an installer script. Do not add a curl-to-root installer until these steps are implemented, tested, and reviewed.

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
- SQLite, via Rhumbase's embedded Go driver
- Rhumbase binaries:
  - `rhumbase`
  - `rhumbased`

The installer must check each dependency and print actionable errors when something is missing.

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

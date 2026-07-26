# SSHDock examples

The maintained public suite is organized around four deployment questions: which framework to deploy, which recognizable software to run, how to connect databases safely, and how to exercise SSHDock workflows.

Every example is a Git-pushable Compose application. Replace `example.com` with the base domain configured by `sudo sshdock server domain set <domain>`. Before a release includes an example, copy it explicitly from `main`; after release promotion, use the matching release tag. Each example README is the source of truth for its required config, deploy, verification, operation, upgrade, cleanup, persistence, limitations, and security boundary.

Framework compatibility probes are executable point-in-time checks, not application starters. They generate or fetch pinned official source only during image build; their repository surface is the Dockerfile, root Compose file, and README. Create a real application through the framework's official tooling and carry over the production Compose requirements demonstrated by the probe.

## Framework quickstarts

- [Next.js](../examples/frameworks/nextjs/README.md) — production Node application from the official generator.
- [NestJS](../examples/frameworks/nestjs/README.md) — production API from the official CLI.
- [Laravel](../examples/frameworks/laravel/README.md) — FrankenPHP application with required app config and named storage.
- [Gin](../examples/frameworks/gin/README.md) — compiled service from pinned official Gin example source.
- [Phoenix LiveView](../examples/frameworks/phoenix/README.md) — generated LiveView application with secure-WebSocket and reconnect acceptance.

## Software recipes

Recipes use pinned official images and the smallest upstream-supported topology. They are deployment envelopes only: no SSHDock-owned application code, themes, plugins, seed data, or custom Dockerfiles.

- [WordPress](../examples/software/wordpress/README.md) — WordPress and MariaDB with private database storage.
- [Gitea](../examples/software/gitea/README.md) — rootless Gitea with persisted repositories.
- [n8n](../examples/software/n8n/README.md) — workflow automation with encrypted state.
- [Memos](../examples/software/memos/README.md) — lightweight persisted notes service.
- [Planka](../examples/software/planka/README.md) — project board with PostgreSQL persistence.

## Database examples

- [PostgreSQL](../examples/databases/postgres/README.md) — loopback-only PostgreSQL accessed through an administrator SSH tunnel; the restricted `sshdock` account does not forward TCP.
- [Shared PostgreSQL](../examples/databases/shared-postgres/README.md) — private clients on an operator-owned external Docker network; it is not an SSHDock-managed service link.

## Feature labs

Feature labs are executable overlays over a canonical example, not duplicate application trees.

- [Config and redeploy](../examples/labs/config-and-redeploy/README.md) — required config, redaction, import, and same-commit redeploy.
- [Failed deploy and Git recovery](../examples/labs/failed-deploy-and-git-recovery/README.md) — failed runtime deployment inspection and Git-selected recovery.
- [Restricted SSH operations](../examples/labs/restricted-ssh-operations/README.md) — lifecycle, exec, run, route, and removal operations.
- [Domains and route check](../examples/labs/domains-and-route-check/README.md) — automatic and manual routes plus active Caddy verification.
- [Health, logs, and history](../examples/labs/health-logs-and-history/README.md) — health, release/deployment history, events, and log follow.
- [Backup, restore, and volume boundary](../examples/labs/backup-restore-and-volume-boundary/README.md) — encrypted config recovery and the boundary that volume contents are not copied.

## Acceptance boundary

The shared contract harness validates the committed public envelope without calling Docker, Caddy, or Git. Docker-backed checks and release dogfood prove the image, Compose, route, and user-visible surfaces. Raw VPS evidence stays private under `.local/`; public docs and issue comments record only the result.

For Compose file selection, project isolation, routing behavior, and the external-file boundary, see [Compose support](COMPOSE_SUPPORT.md). For the full command surface, see [CLI commands](CLI_COMMANDS.md).

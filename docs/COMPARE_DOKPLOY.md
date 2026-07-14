# SSHDock vs Dokploy

This page helps Dokploy-curious users decide whether SSHDock fits their app. It is not a claim that SSHDock replaces Dokploy.

SSHDock is intentionally smaller: Git push a Compose app to one server, operate over SSH, and keep the runtime easy to inspect. Dokploy is a web-managed deployment product with providers, Docker Compose and Docker Stack workflows, UI-managed environment variables, monitoring, logs, deployments, backups, remote servers, and enterprise/team surfaces.

Use SSHDock when you want:

- No web dashboard.
- A single-node Compose runtime.
- Git push as the primary deployment workflow.
- SSH-native day-two operations.
- A small solo-developer PaaS boundary.

Use Dokploy when you want:

- A browser UI for projects, applications, Docker Compose, logs, deployments, domains, and settings.
- Git provider integrations, raw Docker Compose editing, registry pulls, or zip uploads.
- Docker Stack or Swarm-oriented settings.
- Multi-server or team-oriented management.
- Built-in volume backup workflows or cloud/enterprise features.

## Quick Comparison

| Area | SSHDock v0 | Dokploy |
| --- | --- | --- |
| Primary model | Git push Compose apps to one VPS | Web UI for applications, Docker Compose, providers, remote servers, and supporting services |
| Deploy trigger | Git push to SSHDock's Git SSH endpoint | Provider integrations, Git repositories, Docker images, zip uploads, raw Docker Compose, and webhooks |
| Runtime | Single-node Docker Compose | Docker Compose and Docker Stack workflows |
| Operations | `sudo sshdock ...` and `ssh dashboard@<host>` | Browser UI plus configured provider/server workflows |
| Web UI | None | Core part of the product |
| Config/secrets | `.sshdock.yml` declares required keys; encrypted values are stored by SSHDock | UI-defined environment variables for applications and Docker Compose |
| Domains | Base-domain defaults plus `sshdock domains attach/list/check` | UI-managed domains for applications and Docker Compose |
| Logs and deploy history | CLI and SSH dashboard show logs, releases, deployments, events, and failure detail | UI log viewer and deployment history |
| Multi-server | Not in v0 | Documented remote-server and cluster-oriented surfaces |
| Backups | SSHDock host-state backup; Docker volume inventory only by default | Dokploy has backup and volume-backup features for supported setups |
| Teams/RBAC | Not in v0 | Team/enterprise surfaces exist in Dokploy docs |

## What Migrates Well

Good candidates:

- A Dokploy Docker Compose service whose compose file can be committed to Git.
- A simple application whose deployment can move from provider polling/webhooks to `git push sshdock main`.
- Apps that fit one conventional root Compose file and SSHDock's external-file boundary.
- Apps where secrets can move from Dokploy environment settings to SSHDock config.

Poor candidates:

- Apps that depend on Dokploy's web UI as the primary operating surface.
- Docker Stack or Swarm-specific deployments.
- Multi-server deployments.
- Apps depending on Dokploy's provider automation, backups, schedules, notifications, or team features.

## Migration Pointer

Use [`MIGRATE_FROM_DOKPLOY.md`](MIGRATE_FROM_DOKPLOY.md) for a practical migration path. The short version:

1. Export or copy the Compose file into a Git repo.
2. Keep the application in one conventional root Compose file without top-level `include` or external `extends.file` references.
3. Move UI-managed environment values into SSHDock config.
4. Push to SSHDock.
5. Verify health, route, logs, rollback, and backup before DNS cutover.

## Upstream References

Verify upstream behavior before making a production decision:

- Dokploy Docker Compose docs: <https://docs.dokploy.com/docs/core/docker-compose>
- Dokploy providers: <https://docs.dokploy.com/docs/core/providers>
- Dokploy advanced application settings: <https://docs.dokploy.com/docs/core/applications/advanced>
- Dokploy product page: <https://dokploy.com/>

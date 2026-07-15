# SSHDock vs Dokku

This page helps Dokku users decide whether SSHDock is worth trying. SSHDock is not a drop-in Dokku replacement.

SSHDock fits a narrower shape: one VPS, Docker Compose apps, Git push deploys, Caddy routes, SQLite state, and operation through SSH. Dokku is a broader and older app platform with builders, plugins, schedulers, process management, and a larger operational surface.

Use SSHDock when you want:

- A Compose-first app model.
- One node and one operator.
- Git push deploys without a web dashboard.
- SSH-native operations through `sshdock` and `ssh dashboard@<host>`.
- A small implementation that is easy to inspect.

Stay on Dokku, or evaluate it first, when you need:

- Heroku-style buildpacks, Procfiles, or builder selection.
- Dokku plugins and ecosystem integrations.
- Mature multi-process management beyond Compose services.
- A larger production-tested release surface today.
- Scheduler options outside SSHDock v0's single-node Compose runtime.

## Quick Comparison

| Area | SSHDock v0 | Dokku |
| --- | --- | --- |
| Primary model | Docker Compose app in one Git repo | App platform with Git deployment, builders, plugins, and process management |
| Deploy trigger | `git push sshdock main` to `git@sshdock.<domain>:<app>.git` | Git push to Dokku remotes, plus Dokku commands and plugins |
| Runtime | Single-node Docker Compose | Docker-based app runtime with broader scheduler and plugin options |
| Build path | Compose `image` or `build` in `compose.yml` | Buildpacks, Dockerfile, cloud native buildpacks, and other builders |
| Operations | `sudo sshdock ...` and SSH dashboard | `dokku ...` CLI and plugin commands |
| Web UI | None | None in core Dokku |
| Config/secrets | Compose interpolation declares requirements; flat values are stored encrypted and set through SSH or local CLI | `dokku config:set` and related config commands |
| Domains | Base-domain default route, `domains attach`, `domains list`, `domains check` | Domains plugin and proxy/SSL configuration |
| Logs | `sshdock logs <app> [service] --tail <n>` | Dokku log commands |
| Rollback | Release list plus `sshdock apps rollback <app> <release-id>` | Dokku release and rollback tooling |
| Data volumes | Compose volumes are preserved on app removal; backups include volume inventory, not volume contents | Dokku persistent-storage guidance and storage plugin workflows |
| Backup/restore | `sshdock backup create`, `inspect`, and `restore` for SSHDock host state | Dokku backup guidance emphasizes repo copies, static assets, database dumps, and testing restores |
| Users/RBAC | One admin key model for v0 | Dokku has its own user and SSH access model |

## What Migrates Well

Good candidates:

- Apps already described by `compose.yml` or easy to express as Compose.
- Simple Dockerfile apps that can become a Compose `build` service.
- One public web service plus private worker or database services.
- Apps whose state can be moved through database dumps or Docker volume copy workflows.

Poor candidates:

- Apps that depend heavily on Dokku plugins.
- Procfile/buildpack apps that you do not want to convert to Dockerfile or Compose.
- Apps that rely on Dokku-specific process scaling, checks, or hook behavior.
- Production apps where you cannot tolerate v0 rough edges or breaking changes.

## Migration Pointer

Use [`MIGRATE_FROM_DOKKU.md`](MIGRATE_FROM_DOKKU.md) for a practical migration path. The short version:

1. Convert the app shape to `compose.yml`.
2. Declare required config with native Compose interpolation, not committed values.
3. Set config through `ssh dashboard@sshdock.<domain> config ...`.
4. Push to SSHDock.
5. Verify health, route, logs, events, rollback, and backup before DNS cutover.

## Upstream References

Verify upstream behavior before making a production decision:

- Dokku buildpacks and Dockerfile builders: <https://dokku.com/docs/deployment/builders/herokuish-buildpacks/> and <https://dokku.com/docs/deployment/builders/dockerfiles/>
- Dokku Git deployment: <https://dokku.com/docs/deployment/methods/git/>
- Dokku domains: <https://dokku.com/docs/configuration/domains/>
- Dokku persistent storage: <https://dokku.com/docs/advanced-usage/persistent-storage/>
- Dokku backup and recovery: <https://dokku.com/docs/advanced-usage/backup-recovery/>

# AGENTS.md

## Project

Project name: SSHDock

SSHDock is an SSH-native PaaS for solo developers.

North-star:

> Git push Compose apps. Operate over SSH.

## Core Pillars

1. Heroku-style app lifecycle
   - Git push deploys the app.
   - The platform handles build, release, run, config, domains, HTTPS, logs, app status, restart, release history, and rollback.

2. Compose as the app contract
   - One repo represents one full-stack app.
   - The app is defined by `compose.yml` or `docker-compose.yml`.
   - The platform treats the whole Compose stack as the app unit.

3. SSH-only TUI operations
   - No web dashboard.
   - No exposed admin panel.
   - Users operate the platform through `ssh dashboard@server`.

## v0 Scope

v0 is single-node only.

Runtime:

- Go
- Docker Engine
- Docker Compose
- Caddy
- SQLite
- SSH TUI

Do not introduce:

- Web dashboard
- Kubernetes
- k3s
- Docker Swarm
- Multi-node scheduling
- Teams/RBAC
- Marketplace
- Hosted cloud features
- AI assistant

## Product References

Use these as product references, not code sources:

- Coolify: <https://github.com/coollabsio/coolify>
- Dokku: <https://github.com/dokku/dokku>
- CapRover: <https://github.com/caprover/caprover>
- Dokploy: <https://github.com/dokploy/dokploy>

Reference lessons:

- Coolify: broad self-hosted PaaS convenience.
- Dokku: excellent Heroku-like git push deployment.
- CapRover: simple app/database deployment and one-click app flow.
- Dokploy: modern Compose-friendly PaaS.

SSHDock should not become a clone of these tools.

SSHDock should stay:

- SSH-native
- Compose-first
- Git-push deployable
- Single-node-first
- Small, inspectable, and boring in the good way

## Engineering Rules

- Prefer simple Go packages.
- Keep runtime integrations behind interfaces.
- Do not call real Docker, Caddy, or Git from unit tests.
- Use fake runners in tests.
- Keep v0 single-node, but model apps as assigned to a node.
- Do not hardcode paths outside config.
- Keep error messages actionable.
- Store important runtime changes as events or release records.
- Do not add new product pillars without updating `PRD.md`.

## Expected Repo Layout

- `cmd/sshdock/`: user/admin CLI
- `cmd/sshdockd/`: daemon
- `internal/app/`: app registry and lifecycle models
- `internal/gitrecv/`: git push receiver
- `internal/compose/`: Docker Compose validation and deploy runner
- `internal/router/`: Caddy/domain routing
- `internal/tui/`: SSH TUI screens
- `internal/store/`: SQLite persistence
- `internal/harness/`: fake runners and test helpers
- `examples/`: sample apps
- `docs/`: product and architecture docs

## Commands

Use these commands:

```bash
make setup
make fmt
make lint
make test
make smoke
make ci
```

A task is not done until:

```bash
make ci
```

passes.

## Testing Rules

Tests should cover:

- App creation
- Compose file detection
- Release creation
- Fake deploy success
- Fake deploy failure
- Domain attachment
- Rollback state
- TUI view-model rendering

Prefer table-driven Go tests.

## Done Means

For every implementation task:

1. Code compiles.
2. Tests pass.
3. `make ci` passes.
4. Docs are updated if behavior changes.
5. No unsupported platform scope is added.

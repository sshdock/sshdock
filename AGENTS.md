# AGENTS.md

This is the public operating contract for agents and contributors working on SSHDock.

Keep this file stable, concise, and free of private deployment details. Product planning notes, raw VPS evidence, screenshots, command transcripts, and reference research belong in `.local/`, which is intentionally ignored by Git.

## Product Guardrails

SSHDock is an SSH-native PaaS for solo developers.

North-star:

> Git push Compose apps. Operate over SSH.

v0 is single-node only:

- Go
- Docker Engine
- Docker Compose
- Caddy
- SQLite
- SSH TUI

SSHDock must stay:

- SSH-native
- Compose-first
- Git-push deployable
- Single-node-first
- Small, inspectable, and boring in the good way

Do not introduce as current v0 implementation scope:

- Web dashboard
- Kubernetes, k3s, Docker Swarm, or multi-node scheduling
- Teams, RBAC, marketplace, hosted cloud features, or AI assistant features
- Product pillars that are not reflected in public docs and local planning notes

Docs may discuss future runtime-engine exploration only when clearly non-promissory and reflected in both public docs and local planning notes. Do not implement a Kubernetes or k3s backend unless an approved future milestone explicitly changes the v0 runtime scope.

## Engineering Rules

- Prefer simple Go packages.
- Keep runtime integrations behind interfaces.
- Do not call real Docker, Caddy, or Git from unit tests.
- Use fake runners in tests.
- Keep v0 single-node, but model apps as assigned to a node.
- Do not hardcode paths outside config.
- Keep error messages actionable.
- Store important runtime changes as events or release records.
- Update public docs when user-facing behavior changes.

## Repo Layout

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
- `docs/`: public install, command, and operations docs

## Public And Local Docs

Public docs should explain how to install, use, verify, and contribute to SSHDock.

Keep local-only material under `.local/`:

- PRD, task tracker, architecture notes, and reference research
- VPS evidence, screenshots, command transcripts, raw logs, and private domains
- Personal workflow notes or temporary release checklists

Do not add private hostnames, IPs, SSH key paths, fingerprints, backup paths, hashes, or raw development logs to tracked files.

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
4. Public docs are updated if behavior changes.
5. No unsupported platform scope is added.

# Rhumbase

Git push Compose apps. Operate over SSH.

Rhumbase is an SSH-native PaaS for solo developers who want Heroku-style deployments for Docker Compose apps without a web dashboard.

It is designed for one VPS, full-stack Compose apps, and terminal-first operations.

## Core Idea

Rhumbase has three pillars:

1. Heroku-style app lifecycle
   - Git push deploys the app.
   - Rhumbase handles build, release, run, config, domains, HTTPS, logs, status, restart, release history, and rollback.

2. Compose as the app contract
   - One repo represents one full-stack app.
   - The app is defined by `compose.yml` or `docker-compose.yml`.
   - The whole Compose stack is treated as the app unit.

3. SSH-only TUI operations
   - No web dashboard.
   - No exposed admin panel.
   - Operate through:

```bash
ssh dashboard@server
```

## What Rhumbase Is

Rhumbase is:

- A single-node PaaS for solo developers
- A Heroku-like deploy flow for Compose apps
- An SSH-native operator dashboard
- A small, inspectable app lifecycle layer around Docker Compose
- A tool for running several apps on one VPS without turning the VPS into a tiny cloud bureaucracy

## What Rhumbase Is Not

Rhumbase is not:

- A web dashboard
- A Kubernetes platform
- A k3s platform
- A Docker Swarm platform
- A Coolify clone
- A Dokploy clone
- A CapRover clone
- A generic Docker control panel
- An enterprise PaaS
- A hosted cloud product
- A multi-node HA platform in v0

## Example Workflow

Install Rhumbase on the server:

```bash
wget -O bootstrap.sh https://raw.githubusercontent.com/iketiunn/rhumbase/v0.1.0/scripts/bootstrap.sh
sudo RHUMBASE_TAG=v0.1.0 bash bootstrap.sh
sudo rhumbase diagnostics
```

Configure the server Git host and authorize your key:

```bash
sudo rhumbase server domain set rhumbase.example.com
cat ~/.ssh/authorized_keys | sudo rhumbase ssh-keys add admin
```

Add the Git remote:

```bash
git remote add rhumbase git@rhumbase.example.com:my-app.git
```

Deploy:

```bash
git push rhumbase main
```

Open the interactive SSH dashboard:

```bash
ssh dashboard@server
```

Useful dashboard keys are shown in the bottom command bar. `j`/`k` or arrows select apps, `/` filters the app table, `g`/`G` jumps to the first or last app, `tab` switches detail tabs, `u`/`d` scrolls logs, `r` refreshes, `q` quits, and `?` expands help.

Attach a domain:

```bash
rhumbase domains attach my-app web example.com --port 3000
```

For v0, Caddy runs on the host and proxies to a loopback-published Compose port. The app Compose file must publish the selected service on `127.0.0.1:<port>`, and the `--port` value is that host port:

```yaml
services:
  web:
    image: nginx:alpine
    ports:
      - "127.0.0.1:3000:80"
```

Current MVP state:

- `rhumbase` persists app and domain metadata in SQLite.
- `rhumbase apps create` creates a local bare Git repository and installs a `post-receive` hook.
- Local Git pushes can drive `rhumbased git-hook` and record releases/deployments when `rhumbased` is on `PATH`.
- `rhumbased git-receive` supports OpenSSH forced-command push-to-create for flat `<app>.git` paths; the installer hands off from the locked-down `git` SSH user to the `rhumbase` daemon user through a narrow sudoers rule.
- `RHUMBASE_COMPOSE_RUNNER=docker` enables real Docker Compose deployment through `rhumbased git-hook`.
- `scripts/bootstrap.sh` installs Ubuntu/Debian dependencies by default, installs local or released binaries, writes `rhumbased.service`, configures the Caddy import, normalizes runtime ownership, and can be tested under a fake root with `make bootstrap-e2e`.
- `rhumbase server domain set <domain>` persists the Git host used in app remote output.
- `rhumbase ssh-keys add <name>` reads an SSH public key from stdin, stores it in SQLite, and rewrites both Git receive and dashboard `authorized_keys` files with forced commands.
- First push through real OpenSSH can create an app, receive Git, run the generated `post-receive` hook, deploy with fake or Docker Compose runners, and record app/release/deployment/event state.
- `rhumbase domains attach <app> <service> <domain> --port <host-port>` persists the domain, rebuilds the generated Caddyfile from SQLite, validates it, reloads Caddy, and records domain/router events.
- `ssh dashboard@server` uses host OpenSSH on port 22 with a forced `rhumbased dashboard` command, opens an interactive TUI with responsive column tables, K9s-style command tips, app filtering, detail tabs, log scrolling, refresh, and jump keys when a PTY is allocated, and keeps `ssh -T dashboard@server` as a plain text fallback.
- `rhumbase apps restart <app> [service]`, `rhumbase apps redeploy <app>`, and `rhumbase apps rollback <app> <release-id>` run through the configured Compose runner and record recovery deployments/events in SQLite.
- `rhumbase diagnostics` checks config, runtime directories, Docker, Docker Compose, Caddy, SSH, Git, and SQLite migrations with actionable pass/fail output.
- Re-running `scripts/bootstrap.sh` replaces binaries while preserving `/var/lib/rhumbase`; dependency setup and Caddy imports are idempotent, and cleanup remains scoped to Rhumbase-managed image tags.
- Local testing can use `RHUMBASE_DATA_DIR` to avoid writing to `/var/lib/rhumbase`.

```bash
RHUMBASE_DATA_DIR=.tmp/rhumbase go run ./cmd/rhumbase apps create my-app
RHUMBASE_DATA_DIR=.tmp/rhumbase go run ./cmd/rhumbase apps list
RHUMBASE_DATA_DIR=.tmp/rhumbase go run ./cmd/rhumbase domains attach my-app web example.com --port 3000
RHUMBASE_DATA_DIR=.tmp/rhumbase go run ./cmd/rhumbase apps rollback my-app rel_<short-sha>
RHUMBASE_DATA_DIR=.tmp/rhumbase go run ./cmd/rhumbase diagnostics
```

Manual app creation remains available for scripting and debugging:

```bash
rhumbase apps create my-app
```

The v0 Git URL format is intentionally flat:

```text
git@<server-domain>:<app>.git
```

Namespace paths such as `git@<server-domain>:<owner>/<repo>.git` are future work because they require owner-aware SSH key authorization.

Target result when runtime milestones are complete:

- Code is received through Git
- Compose file is detected
- Release is created
- Services with `image` are pulled
- Services with `build` are built through Docker Compose into Rhumbase commit-tagged images using an internal release override file
- Compose stack starts
- Domain routes to the selected service
- HTTPS is handled through Caddy
- App status appears in the SSH TUI

## Supported Compose Files

Rhumbase v0 supports:

- `compose.yml`
- `docker-compose.yml`

Supported Compose features for v0:

- `services`
- `build`
- `image`
- `environment`
- `env_file`
- `depends_on`
- `volumes`
- `ports`
- `expose`
- `healthcheck`
- `restart`

Full Docker Compose compatibility is not a v0 goal.

Unsupported or risky Compose features should fail with clear errors.

## Runtime Stack

v0 runtime:

- Go
- Docker Engine
- Docker Compose
- Caddy
- SQLite
- SSH TUI

v0 is single-node only.

Future multi-node support may be considered later as app placement, not full high availability.

## Target Users

Rhumbase is for:

- Solo developers
- Indie hackers
- Technical founders
- Small-agency developers
- Self-hosters running one VPS
- Developers who like Dokku but want Compose-first apps
- Developers who dislike exposed web admin dashboards

Rhumbase is not for:

- Enterprise teams
- Kubernetes-heavy teams
- Large teams needing RBAC, audit logs, and SSO
- Non-technical WordPress users
- Users needing HA from day one

## Reference Products

Rhumbase is inspired by these tools, but should not become a clone of them:

- Coolify: https://github.com/coollabsio/coolify
- Dokku: https://github.com/dokku/dokku
- CapRover: https://github.com/caprover/caprover
- Dokploy: https://github.com/Dokploy/dokploy

Reference lessons:

- Coolify: broad self-hosted PaaS convenience
- Dokku: excellent Heroku-like Git push deployment
- CapRover: simple app/database deployment and one-click app flow
- Dokploy: modern Compose-friendly self-hosted PaaS

Rhumbase tries to sit in the gap:

- More stack-aware than Dokku
- More deploy-product-like than Dockge
- Less dashboard-heavy than Coolify/Dokploy/CapRover
- More lifecycle-aware than raw Docker Compose

## Repo Layout

Expected layout:

```text
cmd/
  rhumbase/
  rhumbased/

internal/
  app/
  cli/
  compose/
  config/
  gitrecv/
  harness/
  router/
  store/
  tui/

examples/
  node-postgres/
  wordpress-lite/

test/
  fixtures/
  harness/

docs/
```

## Development

Setup:

```bash
make setup
```

Format:

```bash
make fmt
```

Lint:

```bash
make lint
```

Test:

```bash
make test
```

Smoke test:

```bash
make smoke
```

Real Git-hook e2e with fake Compose:

```bash
make e2e
```

Real OpenSSH push e2e with fake Compose:

```bash
make ssh-e2e
```

Opt-in real Docker Compose e2e:

```bash
make e2e-docker
```

Bootstrap installer harness:

```bash
make bootstrap-e2e
```

Bootstrapped server push e2e:

```bash
make server-push-e2e
```

Caddy route e2e:

```bash
make route-e2e
```

SSH dashboard e2e:

```bash
make tui-e2e
```

Recovery e2e:

```bash
make recovery-e2e
```

Hardening e2e:

```bash
make hardening-e2e
```

Full CI:

```bash
make ci
```

A task is not done until:

```bash
make ci
```

passes.

## Testing Philosophy

Unit tests should not require:

- Real Docker
- Real Caddy
- Real Git server
- Real SSH server

Use fake adapters for:

- Compose runner
- Router
- Git push events
- Store fixtures
- TUI view models

Integration tests are tiered. `make e2e` uses real local Git with fake Compose, while `make e2e-docker` also uses the local Docker daemon.

## Docs

Important docs:

- `AGENTS.md`: Codex and contributor instructions
- `PRD.md`: product requirements
- `ARCHITECTURE.md`: system design
- `TASKS.md`: MVP implementation tasks
- `REFERENCES.md`: competitor and technical references

## Product Principle

Rhumbase should stay small, sharp, and boring in the good way.

If a feature makes Rhumbase feel like a generic platform, a web control panel, or Kubernetes hidden behind fake simplicity, it probably does not belong in v0.

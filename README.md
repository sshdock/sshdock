# SSHDock

Git push Compose apps. Operate over SSH.

**Under active development:** SSHDock is good for testing and dogfooding, but it is not for production yet. Expect rough edges and breaking changes.

SSHDock is an SSH-native PaaS for solo developers running Docker Compose apps on one VPS. It gives you a Heroku-like app lifecycle without a web dashboard: install once, push over Git SSH, let SSHDock run Compose, route through Caddy, and operate from the terminal.

v0 is intentionally small:

- single node
- Docker Engine and Docker Compose
- Caddy
- SQLite
- OpenSSH
- SSH dashboard

SSHDock is not a Kubernetes platform, hosted cloud product, multi-node scheduler, team/RBAC system, marketplace, or web control panel.

## Quick Start

Install SSHDock on a fresh Ubuntu/Debian server:

```bash
wget -O bootstrap.sh https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/scripts/bootstrap.sh
sudo SSHDOCK_TAG=v0.3.1 bash bootstrap.sh
```

Set the base domain and authorize your deploy/operator key:

```bash
sudo sshdock server domain set example.com
cat ~/.ssh/id_ed25519.pub | sudo sshdock ssh-keys add admin
```

Point DNS at the server before deploying:

```text
sshdock.example.com  A/AAAA  <server-ip>
*.example.com        A/AAAA  <server-ip>
```

Check the install:

```bash
sudo sshdock diagnostics
```

Add the Git remote from your app repo:

```bash
git remote add sshdock git@sshdock.example.com:my-app.git
```

Your app repo needs exactly one root `compose.yaml`, `compose.yml`, `docker-compose.yaml`, or `docker-compose.yml`. A minimal static site can be:

```yaml
services:
  web:
    image: nginx:alpine
    ports:
      - "127.0.0.1:3000:80"
```

See [`docs/COMPOSE_SUPPORT.md`](docs/COMPOSE_SUPPORT.md) for file detection, Compose authority, and the external-file boundary.

Deploy:

```bash
git push sshdock main
```

After the daemon completes a successful deployment, SSHDock can route the app at:

```text
https://my-app.example.com
```

Deploys use native Compose behavior: validate the effective model, pull images, build services, then run bounded `docker compose up -d --wait`. Services with health checks must become healthy; services without one must remain running. A failed replacement is recorded without automatic rollback, and an existing route is not a zero-downtime traffic switch.

Remote `main` is the desired source revision. Push any local branch, tag, or commit explicitly to remote `main`; other destination refs are rejected. An accepted push records a durable pending deployment, prints its deployment ID, and follows its redacted deployment log while the daemon owns the work. Pressing Ctrl-C or losing the SSH connection detaches only that client; it does not cancel the accepted deployment. Reconnect with `deployments logs <app> <deployment-id> -f`, or use `deployments list <app>` to find the ID.

Git acceptance and deployment completion are separate. A terminal `deploy: failed` does not mean the ref was rejected and does not rewrite remote `main`; inspect `deployments list`, `deployments logs`, `apps health`, and `events list` for the authoritative result and recovery guidance.

SSHDock accepts only one pending or active deployment per app. A second push to that app is rejected before its ref changes. Pushes for other apps are accepted and queued; the persistent daemon runs queued push deployments one at a time. Pending attempts survive a daemon restart. An attempt interrupted while actively deploying is marked failed with retry guidance and is not silently rerun.

SSHDock warns when trusted Compose input publishes on all interfaces or couples directly to the host through privileged mode, host networking, bind mounts, the Docker socket, explicit global volume names, or external volumes. These warnings do not provide a sandbox; only trusted owners should have deploy access.

## Day-One Commands

Open the SSH dashboard:

```bash
ssh sshdock@sshdock.example.com
```

Inspect apps and app state:

```bash
ssh sshdock@sshdock.example.com apps list
ssh sshdock@sshdock.example.com apps info my-app
ssh sshdock@sshdock.example.com apps health my-app
ssh sshdock@sshdock.example.com logs my-app --tail 200
ssh sshdock@sshdock.example.com releases list my-app
ssh sshdock@sshdock.example.com deployments list my-app
ssh sshdock@sshdock.example.com deployments logs my-app -f
ssh sshdock@sshdock.example.com deployments logs my-app dep_... -f
ssh sshdock@sshdock.example.com events list my-app
```

Each app commit has one stable release while every push or redeploy records a separate deployment attempt. `deployments list` prints the complete attempt history, including timing and redacted failure recovery detail. `deployments logs` retrieves durable deployment-stage output; it is separate from container logs, retains the newest 20 logs per app, and caps each log at 10 MiB.

Operate an app:

```bash
ssh sshdock@sshdock.example.com apps stop my-app
ssh sshdock@sshdock.example.com apps start my-app
ssh sshdock@sshdock.example.com apps restart my-app
ssh -tt sshdock@sshdock.example.com apps exec my-app web -- sh
ssh sshdock@sshdock.example.com apps run my-app web -- ./bin/migrate up
ssh sshdock@sshdock.example.com apps redeploy my-app
ssh sshdock@sshdock.example.com apps remove my-app --force
```

`apps stop` preserves the existing Compose containers, networks, and volumes. `apps start` starts those existing containers; if they no longer exist, SSHDock tells you to redeploy current remote `main`. `apps restart` uses Compose restart and does not apply changed Compose or config values. These commands resolve the Compose file in the current app worktree rather than selecting a historical release. `apps exec` attaches to an existing service container; `apps run` starts a removable one-off Compose container. The required `--` keeps container argv separate from SSHDock and Compose flags.

`apps redeploy` retries the commit currently stored at remote `main`, creating another deployment attempt even when that commit was deployed before. To select an older revision with Git, push it to remote `main`:

```bash
git push --force sshdock <commit-or-lightweight-tag>:main
git push --force sshdock '<annotated-tag>^{}:refs/heads/main'
```

On startup, `sshdockd` resumes pending push deployments but never silently reruns an attempt interrupted while actively deploying. Docker Compose restart policies own container recovery after Docker or host reboot. `apps health` and `diagnostics` warn when a routed or running service still uses Compose's default non-restarting policy; use `restart: unless-stopped` or `restart: always` when that service should return after reboot.

Manual domain attach is available when auto-routing is not enough:

```bash
sudo sshdock domains attach my-app web app.example.com --port 3000
sudo sshdock domains list my-app
sudo sshdock domains check my-app
```

Remove an app without deleting Docker volumes:

```bash
sudo sshdock apps remove my-app
```

Removal deletes live app state but retains its started, failed, and succeeded audit events. Reusing the same app name keeps that earlier audit history visible.

Create and inspect a host-state backup:

```bash
sudo sshdock backup create
sudo sshdock backup inspect /var/lib/sshdock/backups/<archive>.tar.gz
```

## App Config

Store app config outside Git and reference required values with native Compose interpolation:

```yaml
services:
  web:
    environment:
      DATABASE_URL: ${DATABASE_URL:?set DATABASE_URL with sshdock config set}
```

Store values over SSH, then deploy or redeploy:

```bash
ssh sshdock@sshdock.example.com config set my-app DATABASE_URL < database-url.txt
ssh sshdock@sshdock.example.com config list my-app
sudo sshdock apps redeploy my-app
```

Config values are encrypted in SQLite with a host-local key outside the database. Back up the SQLite database and config key together.

See [`docs/EXAMPLES.md`](docs/EXAMPLES.md) for maintained framework compatibility probes, software recipes, database examples, and executable feature labs. See [`docs/CLI_COMMANDS.md`](docs/CLI_COMMANDS.md) for the full config command reference.

## Docs

- [`docs/INSTALL.md`](docs/INSTALL.md): install, upgrade, firewall, backup, and restore notes.
- [`docs/CLI_COMMANDS.md`](docs/CLI_COMMANDS.md): complete `sshdock` and `sshdockd` command reference.
- [`docs/EXAMPLES.md`](docs/EXAMPLES.md): runnable user-story examples.
- Adoption docs: [`docs/COMPARE_DOKKU.md`](docs/COMPARE_DOKKU.md), [`docs/COMPARE_DOKPLOY.md`](docs/COMPARE_DOKPLOY.md), [`docs/MIGRATE_FROM_DOKKU.md`](docs/MIGRATE_FROM_DOKKU.md), [`docs/MIGRATE_FROM_DOKPLOY.md`](docs/MIGRATE_FROM_DOKPLOY.md), and [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md).
- [`docs/RUNTIME_ENGINES.md`](docs/RUNTIME_ENGINES.md): current Compose runtime and future engine boundary.
- [`docs/TESTING.md`](docs/TESTING.md): test tiers, e2e harnesses, and contributor verification.
- [`AGENTS.md`](AGENTS.md): contributor and agent operating contract.

Private planning notes and release evidence live under `.local/`, which is intentionally ignored by Git.

## Development

Set up local tooling:

```bash
make setup
```

Run the full repository gate:

```bash
make ci
```

Useful focused checks:

```bash
make test
make smoke
make config-e2e
make bootstrap-e2e
```

Unit tests use fake adapters for Docker Compose, Caddy, Git, SSH, and TUI state. Integration tests are tiered so normal development does not require a real server.

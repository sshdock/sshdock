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

Set the base domain and authorize your deploy/dashboard key:

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

Your app repo needs `compose.yml` or `docker-compose.yml`. A minimal static site can be:

```yaml
services:
  web:
    image: nginx:alpine
    ports:
      - "127.0.0.1:3000:80"
```

See [`docs/COMPOSE_SUPPORT.md`](docs/COMPOSE_SUPPORT.md) for the supported Compose subset and known unsupported fields.

Deploy:

```bash
git push sshdock main
```

After a successful deploy, SSHDock can route the app at:

```text
https://my-app.example.com
```

## Day-One Commands

Open the SSH dashboard:

```bash
ssh dashboard@sshdock.example.com
```

Inspect apps and app state:

```bash
sudo sshdock apps list
sudo sshdock apps info my-app
sudo sshdock logs my-app
sudo sshdock releases list my-app
sudo sshdock events list my-app
```

Failed deploys print and persist `stage`, `detail`, `changed`, `fix`, and `retry` fields. The same failure detail is visible through `releases list`, `events list`, and the SSH dashboard, with stored config values redacted.

Operate an app:

```bash
sudo sshdock apps restart my-app
sudo sshdock apps redeploy my-app
sudo sshdock apps rollback my-app <release-id>
```

Manual domain attach is available when auto-routing is not enough:

```bash
sudo sshdock domains attach my-app web app.example.com --port 3000
sudo sshdock domains list my-app
```

Remove an app without deleting Docker volumes:

```bash
sudo sshdock apps remove my-app
```

## App Config

For releases with app config support, apps can commit `.sshdock.yml` to declare required config keys without committing secret values:

```yaml
config:
  required:
    - DATABASE_URL
```

Store values over SSH, then deploy or redeploy:

```bash
ssh dashboard@sshdock.example.com config set my-app DATABASE_URL < database-url.txt
ssh dashboard@sshdock.example.com config list my-app
sudo sshdock apps redeploy my-app
```

Config values are encrypted in SQLite with a host-local key outside the database. Back up the SQLite database and config key together.

See [`docs/EXAMPLES.md`](docs/EXAMPLES.md) for runnable examples covering static sites, build services, config-backed apps, workers, Redis, Postgres, stateful volumes, and rollback. See [`docs/CLI_COMMANDS.md`](docs/CLI_COMMANDS.md) for the full config command reference.

## Docs

- [`docs/INSTALL.md`](docs/INSTALL.md): install, upgrade, firewall, backup, and restore notes.
- [`docs/CLI_COMMANDS.md`](docs/CLI_COMMANDS.md): complete `sshdock` and `sshdockd` command reference.
- [`docs/EXAMPLES.md`](docs/EXAMPLES.md): runnable user-story examples.
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

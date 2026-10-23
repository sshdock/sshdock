# SSHDock

Git push Compose apps. Operate over SSH.

SSHDock targets a trusted operator on one Linux VPS. See the [v1.1 candidate validation](docs/V1_1_ACCEPTANCE.md) for current-source checks and limits; [v1.0.0 validation](docs/RELEASE_ACCEPTANCE.md) remains historical.

SSHDock is an SSH-native PaaS for solo developers running Docker Compose apps on one VPS. It gives you a Heroku-like app lifecycle without a web dashboard: install once, push over Git SSH, let SSHDock run Compose, route through Caddy, and operate from the terminal.

SSHDock stays small:

- single node
- Docker Engine and Docker Compose
- Caddy
- SQLite
- OpenSSH
- SSH dashboard

SSHDock is not a Kubernetes platform, hosted cloud product, multi-node scheduler, team/RBAC system, marketplace, or web control panel.

## License and operator responsibility

SSHDock is licensed under [Apache-2.0](LICENSE). Third-party components retain their own licenses; see [licensing and provenance](docs/LICENSING.md).

Manual deployment, diagnosis, Git-selected recovery, and host-state backup/restore are free OSS capabilities. They run on your VPS without an SSHDock Cloud account, API, login, or subscription. They do not depend on an SSHDock-operated service; fetching images, dependencies, DNS, and certificates can still require network access.

You own host administration, app availability, application-data consistency, and recovery decisions. There is no automatic rollback or database migration reversal. Host-state archives inventory volumes without copying their contents. Use the separate [manual volume and PostgreSQL backup workflows](docs/APPLICATION_DATA_BACKUPS.md) for application data, and test restoration. See [host operations](docs/INSTALL.md#host-operations-boundary) and the [security boundary](docs/SECURITY.md).

Potential paid hosted operations are future research, not shipped functionality or a restriction on the Apache-2.0 OSS runtime and its manual operations.

## Published Release: v1.1.0

Install [v1.1.0](https://github.com/sshdock/sshdock/releases/tag/v1.1.0) on a single Ubuntu/Debian VPS with its matching bootstrap script and assets:

```bash
wget -O bootstrap.sh https://raw.githubusercontent.com/sshdock/sshdock/v1.1.0/scripts/bootstrap.sh
sudo SSHDOCK_TAG=v1.1.0 bash bootstrap.sh
```

Follow the [v1.1.0 quick start](https://github.com/sshdock/sshdock/blob/v1.1.0/README.md#quick-start) and [v1.1.0 installation guide](https://github.com/sshdock/sshdock/blob/v1.1.0/docs/INSTALL.md). This release uses `ssh sshdock@sshdock.example.com` and includes durable deployment logs. Always take the bootstrap script and binaries from the same release tag.

v1.1 adds structured CLI inspection, read-only preflight, documented Git-selected recovery, manual application-data archives and a portable coding-agent skill. See the [release and upgrade notes](docs/V1_1_RELEASE.md) for availability, installation and operational limits.

## Quick Start

After installation, set the base domain on the server:

```bash
sudo sshdock server domain set example.com
```

From your workstation, authorize one selected deploy/operator public key through your existing administrator SSH connection (replace the account, host and key filename):

```bash
ssh ubuntu@server.example.com 'sudo sshdock ssh-keys add admin' < ~/.ssh/id_ed25519.pub
```

Keep the private key on your workstation and verify the SSH host key through your trusted setup. See [installation setup](docs/INSTALL.md#setup) for details.

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

Verify service health, the active route and public HTTPS separately:

```bash
ssh sshdock@sshdock.example.com apps health my-app
ssh sshdock@sshdock.example.com domains check my-app
curl --fail --show-error --connect-timeout 5 --max-time 20 https://my-app.example.com/
```

A successful deployment records the Compose result; it does not prove that Caddy applied the route or that public DNS/TLS works. Inspect warning/failure details before retrying a deployment. See [routing troubleshooting](docs/TROUBLESHOOTING.md#deploy-fails).

Deploys use native Compose behavior: validate the effective model, pull images, build services, then run bounded `docker compose up -d --wait`. Services with health checks must become healthy; services without one must remain running. A failed replacement is recorded without automatic rollback, and an existing route is not a zero-downtime traffic switch.

You can build on the VPS (**Local Build**) or publish commit-tagged images from your own CI and registry (**External Build**). Both use the same Git push and Compose contract. See [External builds](docs/EXTERNAL_BUILDS.md) for `SSHDOCK_GIT_SHA` and a complete GitHub Actions/GHCR example; this metadata is included in v1.0.0.

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

`apps redeploy` retries the commit currently stored at remote `main`, creating another deployment attempt even when that commit was deployed before. To select an older commit or tag, follow [manual deployment recovery](docs/RECOVERY.md): review the target, confirm its full commit, and push with an explicit expected-main lease. Then verify the new attempt, service health and public route separately. Preserved volumes do not reverse database migrations.

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

For application data, see [manual backups](docs/APPLICATION_DATA_BACKUPS.md): quiesced named-volume export/fresh-volume restore and PostgreSQL-native logical dump/restore. These are separate from host-state archives and are not automatic live snapshots.

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
- [`docs/JSON_OUTPUT.md`](docs/JSON_OUTPUT.md): v1.1 structured inspection and deployment polling contract.
- [`docs/PREFLIGHT.md`](docs/PREFLIGHT.md): v1.1 read-only local and server Compose checks.
- [`docs/AGENT_OPERATIONS.md`](docs/AGENT_OPERATIONS.md): portable Codex/Claude Code skill and approved manual operations.
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

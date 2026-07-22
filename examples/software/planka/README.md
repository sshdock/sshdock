# Planka software recipe

## Purpose

This recipe deploys official Planka and PostgreSQL images through SSHDock without custom application code, board seed data, wrapper scripts, or Dockerfiles. It proves initial sign-in, a persistent board and card, exact-image upgrades, routine operation, and explicit cleanup.

## Prerequisites

- A working SSHDock server with a base domain configured.
- DNS for `*.example.com` pointing at the server.
- Your public key added to the `sshdock` operator account.
- Local `curl`, `git`, `openssl`, and `tar` commands.

Replace `example.com` below with the server's base domain.

## Topology

The root `compose.yml` runs Planka and PostgreSQL in Planka's smallest official production topology. Planka's HTTP port binds to IPv4 loopback at `127.0.0.1:18204`, so Caddy is the only public HTTP and HTTPS entry point. PostgreSQL has no published host port.

`planka-data` preserves Planka's `/app/data` state, and `postgres-data` preserves the Planka database. `TRUST_PROXY=true` lets Planka honor the forwarded protocol from SSHDock's Caddy route. The initial-admin environment values use Planka's documented setup path; keep them in SSHDock config, not in this repository.

## Pinned versions

- Planka: `ghcr.io/plankanban/planka:2.1.1@sha256:19b507ae3ab5cb1855c3f6984249e4a4881ed0b912febdfd492139c29bf10f39`
- PostgreSQL: `postgres:16.14-alpine3.22@sha256:786dab398303b8ce7cb76b407bb21ef2e4dfbbbd4c6abcf3d29b3130467ffdbc`

Planka `2.1.1` is the release selected for this point-in-time compatibility result. The Git-selected upgrade baseline is `ghcr.io/plankanban/planka:2.1.0@sha256:32c919d9e65b0479d1bbf0e4fb8e00fb11403a5234242b9f5fc900e595c2e2ce`. Before changing either pin, review the [Planka release notes](https://github.com/plankanban/planka/releases), its [production Docker guidance](https://docs.planka.cloud/docs/installation/docker/production-version/), and its [upgrade guidance](https://docs.planka.cloud/docs/upgrade-to-v2/docker/).

## Deploy

Until a release tag contains this recipe, copy its two public files from `main`:

```bash
mkdir planka
cd planka
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/software/planka
git init -b main
git add .
git commit -m "Deploy Planka"
git remote add sshdock git@sshdock.example.com:planka.git
git push sshdock main
```

The accepted push creates the app but stops before Compose starts because required values are absent. Store every value through the restricted SSH surface, confirm normal output redacts secrets, then redeploy the same remote `main` commit and attach the conventional route:

```bash
printf '%s' 'https://planka.example.com/' \
  | ssh sshdock@sshdock.example.com config set planka PLANKA_BASE_URL
openssl rand -hex 24 \
  | ssh sshdock@sshdock.example.com config set planka PLANKA_DB_PASSWORD
openssl rand -hex 32 \
  | ssh sshdock@sshdock.example.com config set planka PLANKA_SECRET_KEY
printf '%s' 'admin@example.com' \
  | ssh sshdock@sshdock.example.com config set planka PLANKA_ADMIN_EMAIL
openssl rand -base64 24 \
  | tr -d '\n' \
  | ssh sshdock@sshdock.example.com config set planka PLANKA_ADMIN_PASSWORD
printf '%s' 'SSHDock Admin' \
  | ssh sshdock@sshdock.example.com config set planka PLANKA_ADMIN_NAME
printf '%s' 'sshdock-admin' \
  | ssh sshdock@sshdock.example.com config set planka PLANKA_ADMIN_USERNAME
ssh sshdock@sshdock.example.com config list planka
sudo sshdock apps redeploy planka
sudo sshdock domains attach planka web planka.example.com --port 18204
```

Open `https://planka.example.com` and sign in with the configured initial-admin email and password. On the first sign-in, review and accept Planka's End User Terms of Service, then create a personal project named `SSHDock acceptance project`. Keep those credentials outside this repository.

## Verify

Within **SSHDock acceptance project**, create a board named **SSHDock recipe board**. Add a list such as `Backlog`, then create a card named **SSHDock persistence card**. Confirm the board and card are visible after a page refresh, then inspect the SSHDock surfaces:

```bash
curl -fsS https://planka.example.com
sudo sshdock apps health planka
sudo sshdock domains check planka
sudo sshdock deployments list planka
sudo sshdock events list planka
```

The public HTTPS route responds successfully, SSHDock reports Planka and PostgreSQL healthy, and the route check reports an active Caddy route. The board and card are the application-level proof that both persistent volumes are in use.

## Operate

```bash
sudo sshdock logs planka web --tail 100
ssh sshdock@sshdock.example.com apps exec planka web -- node --version
sudo sshdock apps restart planka
sudo sshdock apps health planka
curl -fsS https://planka.example.com
```

After the restart, sign in again if necessary and confirm **SSHDock recipe board** and **SSHDock persistence card** remain available. `restart: unless-stopped` restores both Compose services after a Docker daemon or host restart; scheduled work belongs to Planka or the Compose application, not SSHDock.

## Upgrade

Back up Planka and PostgreSQL before every upgrade. Review Planka's release and database-migration guidance, update the image line to a newer exact version-and-digest pin, then use the ordinary Git-selected deployment path:

```bash
git add compose.yml README.md
git commit -m "Upgrade Planka image"
git push sshdock main
sudo sshdock apps health planka
sudo sshdock domains check planka
curl -fsS https://planka.example.com
```

After a successful deployment, verify the same public route, **SSHDock recipe board**, and **SSHDock persistence card**. The named volumes remain attached across an exact-image update. Do not attempt a Planka v1-to-v2 migration without following Planka's documented backup and migration procedure.

## Cleanup

Ordinary SSHDock removal deletes app-owned metadata, routes, repositories, worktrees, and containers while preserving Docker volumes:

```bash
sudo sshdock apps remove planka --force
```

Only when you intentionally want to destroy the board, cards, attachments, and database, remove both persistent volumes after removing the app:

```bash
sudo docker volume rm sshdock_planka_planka-data sshdock_planka_postgres-data
```

## Persistence

`planka-data` persists Planka application data at `/app/data`, including uploaded attachments. `postgres-data` persists the PostgreSQL database that holds users, boards, lists, and cards. Restarts, redeploys, exact-image upgrades, and ordinary app removal preserve both named volumes. SSHDock backup inventories Docker volumes but does not copy their contents; make an application-consistent Planka and PostgreSQL backup separately.

## Limitations

This recipe proves the official initial-admin surface, one persistent board and card, HTTPS routing, logs, restart, redeploy, exact-image upgrade, persistence, and cleanup. It does not provide managed Planka upgrades, application-consistent backups, object storage, email delivery, SSO, high availability, or zero-downtime deployment.

## Security boundaries

Planka is public through the attached HTTPS route; PostgreSQL remains private to the Compose network. `OUTGOING_BLOCKED_HOSTS` blocks Planka's internal outbound proxy from reaching `localhost`, `postgres`, and `db`, so outbound integrations and attachment fetches cannot target those internal hosts. Protect the initial-admin account, database password, application secret, Docker and host access, and application backups. SSHDock encrypts configured values at rest, but a host administrator, SSHDock process, Docker runtime, or a workload with access to the Compose environment can read them. SSHDock's trusted-owner model does not sandbox malicious Compose workloads or untrusted Planka integrations.

# n8n software recipe

## Purpose

This recipe deploys the official n8n image through SSHDock without custom application code, workflow seed data, wrapper scripts, or Dockerfiles. It proves first-run setup, a persistent published webhook workflow, exact-image upgrades, routine operation, and explicit cleanup.

## Prerequisites

- A working SSHDock server with a base domain configured.
- DNS for `*.example.com` pointing at the server.
- Your public key added to the `sshdock` operator account.
- Local `curl`, `git`, `openssl`, and `tar` commands.

Replace `example.com` below with the server's base domain.

## Topology

The root `compose.yml` runs one official n8n service using its default SQLite database. Its web port binds to IPv4 loopback at `127.0.0.1:18202`, so Caddy is the only public HTTP and HTTPS entry point. The `n8n-data` named volume holds the upstream `.n8n` directory, including SQLite workflow and execution state.

The official image is the smallest supported one-service topology for this recipe. SQLite needs no additional service. A production reverse proxy requires `WEBHOOK_URL` and `N8N_PROXY_HOPS=1`; SSHDock's Caddy route supplies the forwarded headers. Workflow scheduling belongs to n8n or the Compose application, not SSHDock.

## Pinned versions

- n8n: `docker.n8n.io/n8nio/n8n:2.30.5@sha256:450853cd21a2ce36587c4c860eb26927c1ceba9496bf55f4c213b5d3a6dc8c6f`

This is an exact stable version and multi-platform manifest digest from n8n's official registry. The acceptance upgrade baseline is `2.29.11@sha256:ad826993267f20365b061126717b60d88c81369c2e5f6828b0a188c432cb850f`. Review the [n8n release notes](https://github.com/n8n-io/n8n/releases), [Docker guidance](https://docs.n8n.io/hosting/installation/docker/), and breaking changes before replacing the pin.

## Deploy

Until a release tag contains this recipe, copy its two public files from `main`:

```bash
mkdir n8n
cd n8n
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/software/n8n
git init -b main
git add .
git commit -m "Deploy n8n"
git remote add sshdock git@sshdock.example.com:n8n.git
git push sshdock main
```

The accepted push creates the app but stops before Compose starts because the required values are absent. Store them through the restricted SSH surface, confirm normal output redacts the encryption key, then redeploy current remote `main` and attach the conventional route:

```bash
printf '%s' 'n8n.example.com' \
  | ssh sshdock@sshdock.example.com config set n8n N8N_HOST
printf '%s' 'https://n8n.example.com/' \
  | ssh sshdock@sshdock.example.com config set n8n N8N_WEBHOOK_URL
openssl rand -hex 32 \
  | ssh sshdock@sshdock.example.com config set n8n N8N_ENCRYPTION_KEY
ssh sshdock@sshdock.example.com config list n8n
sudo sshdock apps redeploy n8n
sudo sshdock domains attach n8n web n8n.example.com --port 18202
```

Open `https://n8n.example.com` and create the first owner account. Keep the owner credentials outside this repository.

## Verify

In the n8n editor, create a workflow named `recipe-proof` with a **Webhook** trigger using the production path `recipe-proof`, `GET` method, and the default immediate response. Save and publish the workflow, then request the public HTTPS webhook and inspect the recorded execution:

```bash
curl -fsS https://n8n.example.com/webhook/recipe-proof
sudo sshdock apps health n8n
sudo sshdock domains check n8n
sudo sshdock deployments list n8n
sudo sshdock events list n8n
```

The webhook returns n8n's `{"message":"Workflow was started"}` response, the editor shows its successful execution, and SSHDock reports one healthy service with an active route.

## Operate

```bash
sudo sshdock logs n8n web --tail 100
ssh sshdock@sshdock.example.com apps exec n8n web -- n8n --version
sudo sshdock apps restart n8n
curl -fsS https://n8n.example.com/webhook/recipe-proof
sudo sshdock apps health n8n
```

The restart keeps the named volume, so the published workflow and its webhook execution surface remain available. Schedule Trigger and other timed workflows run in n8n; `restart: unless-stopped` only restores the Compose service after a daemon or host restart.

## Upgrade

Back up the n8n database and review n8n's release notes and migration guidance before changing versions. Replace the image line with a newer exact official version-and-digest pin, then use the normal Git-selected deployment path:

```bash
git add compose.yml
git commit -m "Upgrade n8n image"
git push sshdock main
curl -fsS https://n8n.example.com/webhook/recipe-proof
ssh sshdock@sshdock.example.com apps exec n8n web -- n8n --version
sudo sshdock apps health n8n
```

The representative webhook must still respond and the editor must retain its workflow and execution history after the successful deployment.

## Cleanup

Ordinary removal deletes SSHDock-owned metadata, routes, repositories, worktrees, and containers while preserving the named volume:

```bash
sudo sshdock apps remove n8n --force
```

Remove the persistent n8n state only when you intend to destroy workflows, credentials, and execution history:

```bash
sudo docker volume rm sshdock_n8n_n8n-data
```

## Persistence

`n8n-data` persists the upstream `/home/node/.n8n` directory. With the default SQLite topology, it contains n8n's SQLite database for workflows and executions as well as instance data. `N8N_ENCRYPTION_KEY` is stored separately through SSHDock config; retain it with the named volume and database backup so encrypted n8n credentials remain readable.

## Limitations

This is the smallest single-service SQLite topology. It does not demonstrate queue mode, workers, Redis, external PostgreSQL, high availability, or an SSHDock-managed scheduler. Choose an upstream-supported n8n topology when those requirements apply.

## Security boundaries

n8n's editor and published webhooks are public through the attached HTTPS route. Protect the first owner account, use unique operator and n8n credentials, and review published workflows before exposing their paths. SSHDock encrypts `N8N_ENCRYPTION_KEY` at rest, but a host administrator, SSHDock process, Docker runtime, or a workload with access to the n8n environment can still read it. SSHDock backup inventories Docker volumes but does not copy their contents; make an application-consistent n8n database backup separately.

# Memos software recipe

## Purpose

This recipe deploys the official Memos image through SSHDock without custom application code, seed data, wrapper scripts, or Dockerfiles. It proves first-run setup, a persistent daily-use memo, exact-image upgrades, routine operation, and explicit cleanup.

## Prerequisites

- A working SSHDock server with a base domain configured.
- DNS for `*.example.com` pointing at the server.
- Your public key added to the `sshdock` operator account.
- Local `curl`, `git`, and `tar` commands.

Replace `example.com` below with the server's base domain.

## Topology

The root `compose.yml` runs one official Memos service with its default SQLite storage. Its web port binds to IPv4 loopback at `127.0.0.1:18203`, so Caddy is the only public HTTP and HTTPS entry point. The `memos-data` named volume holds the SQLite database and locally stored attachments under `/var/opt/memos`.

The official image is the smallest supported one-service topology: SQLite needs no additional service. Its released container is built from Alpine, but Memos publishes its official image under an exact version tag rather than a separate variant tag. `MEMOS_INSTANCE_URL` supplies Memos with its public HTTPS URL through SSHDock config.

## Pinned versions

- Memos: `neosmemo/memos:0.29.1@sha256:3e1253477066eb2aefa91145f7f9038bb931ed88c8a3ee05310a933594cdba7d`

This is the exact `v0.29.1` release tag and multi-platform manifest digest from the official Memos image. The VPS upgrade baseline is `0.29.0@sha256:471bd5dab62d59944644e177c366a44a6639584bffa7cacd72ca4d16f53f9a6d`. Recheck the [Memos release notes](https://github.com/usememos/memos/releases), [Docker Compose guide](https://usememos.com/docs/deploy/docker-compose), and manifest digest before changing the pin.

## Deploy

Until a release tag contains this recipe, copy its two public files from `main`:

```bash
mkdir memos
cd memos
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/software/memos
git init -b main
git add .
git commit -m "Deploy Memos"
git remote add sshdock git@sshdock.example.com:memos.git
git push sshdock main
```

The accepted push creates the app but stops before Compose starts because the instance URL is absent. Set the URL through the restricted SSH surface, redeploy current remote `main`, and attach the conventional route:

```bash
printf '%s' 'https://memos.example.com/' \
  | ssh sshdock@sshdock.example.com config set memos MEMOS_INSTANCE_URL
ssh sshdock@sshdock.example.com config list memos
sudo sshdock apps redeploy memos
sudo sshdock domains attach memos web memos.example.com --port 18203
```

Open `https://memos.example.com`, complete Memos' first-owner setup, and create a memo with the exact text `SSHDock persistence proof`.

## Verify

```bash
curl -I http://memos.example.com
curl -fsS https://memos.example.com
sudo sshdock apps health memos
sudo sshdock domains check memos
sudo sshdock deployments list memos
sudo sshdock events list memos
```

HTTP redirects to HTTPS, the public route serves the Memos interface, and the first owner can still see `SSHDock persistence proof` after reloading the page. SSHDock reports one healthy service with an active route.

## Operate

```bash
sudo sshdock logs memos web --tail 100
ssh sshdock@sshdock.example.com apps exec memos web -- /usr/local/memos/memos --version
sudo sshdock apps restart memos
sudo sshdock apps health memos
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://memos.example.com
```

After the restart, sign in again if needed and confirm `SSHDock persistence proof` still appears in the Memos interface.

## Upgrade

Back up Memos before every upgrade. Choose a supported exact Memos version tag, resolve its current multi-platform manifest digest, review the release notes and upgrade guidance, and update `compose.yml` plus the recorded pin above. Commit the pin change so Git selects the new deployment:

```bash
git add compose.yml README.md
git commit -m "Upgrade Memos image"
git push sshdock main
curl -fsS https://memos.example.com
sudo sshdock apps health memos
```

After the deployment succeeds, verify the same public route and `SSHDock persistence proof` memo. The named volume remains attached across an exact-image update.

## Cleanup

Ordinary SSHDock removal deletes app-owned containers and state while preserving Docker volumes:

```bash
sudo sshdock apps remove memos --force
sudo docker volume ls --format '{{.Name}}' | grep '^sshdock_memos_'
```

Only when you intentionally want to destroy Memos data and attachments, remove the volume after removing the app:

```bash
sudo docker volume rm sshdock_memos_memos-data
```

## Persistence

`memos-data` stores Memos' SQLite database and local attachments at `/var/opt/memos`. Restart, redeploy, exact-image upgrades, and ordinary app removal preserve the named volume; SSHDock does not back up its contents.

## Limitations

This recipe proves the upstream first-owner surface, one persistent memo, HTTPS routing, logs, restart, redeploy, exact-image upgrade, persistence, and cleanup. It does not provide managed Memos upgrades, application-consistent backups, object storage, SMTP, multi-user administration, high availability, or zero-downtime deployment.

## Security boundaries

Memos' HTTP port is reachable only through host loopback and Caddy remains the public TLS entry point. `MEMOS_INSTANCE_URL` is public routing metadata stored through SSHDock config; account credentials and any Memos-managed integration credentials stay outside this repository. Protect Docker and host access, keep backups, review Memos security releases, and remember that SSHDock's trusted-owner model does not sandbox malicious Compose workloads or untrusted Memos extensions.

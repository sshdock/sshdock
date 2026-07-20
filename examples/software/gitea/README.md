# Gitea software recipe

## Purpose

This recipe deploys the official Gitea rootless image through SSHDock without custom application code, wrapper scripts, seed data, or Dockerfiles. It proves first-run setup, persistent repositories, real SSH Git operations, exact-image upgrades, routine operation, and explicit cleanup.

## Prerequisites

- A working SSHDock server with a base domain configured.
- DNS for `*.example.com` pointing at the server.
- Your public key added to the `sshdock` operator account.
- Local `curl`, `git`, `openssl`, `ssh`, and `tar` commands.
- Host and provider firewalls prepared for TCP port `18222` if Git over SSH will be used remotely.

Replace `example.com` below with the server's base domain.

## Topology

The root `compose.yml` runs one official rootless Gitea service with its supported SQLite database. The web port binds to IPv4 loopback at `127.0.0.1:18201` so Caddy remains the public HTTP and HTTPS entry point. Gitea's built-in SSH server publishes host port `18222`, avoiding the host OpenSSH and SSHDock Git receive ports. Named volumes hold application data and generated configuration.

The rootless variant is the smallest official one-service topology: SQLite needs no second database service, and the image runs Gitea without container-root privileges. Gitea documents rootless and rootful volume layouts as incompatible, so upgrades must stay on the rootless image family.

## Pinned versions

- Gitea: `docker.gitea.com/gitea:1.27.0-rootless@sha256:414ba5b2b1163480e9ed4213a989cd798579cfa88582a2359303273009b2b852`

This is an exact stable version-and-variant tag with its multi-platform manifest digest from Gitea's official registry. Recheck the [official rootless installation guide](https://docs.gitea.com/installation/install-with-docker-rootless), [release notes](https://github.com/go-gitea/gitea/releases), and manifest digest before changing the pin.

## Deploy

Until a release tag contains this recipe, copy its two public files from `main`:

```bash
mkdir gitea
cd gitea
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/software/gitea
git init -b main
git add .
git commit -m "Deploy Gitea"
git remote add sshdock git@sshdock.example.com:gitea.git
git push sshdock main
```

The accepted push creates the app but stops before Compose starts because the four required values are absent. Store them through the restricted SSH surface, confirm output is redacted, redeploy current remote `main`, and attach the conventional route:

```bash
printf '%s' 'gitea.example.com' \
  | ssh sshdock@sshdock.example.com config set gitea GITEA_DOMAIN
printf '%s' 'https://gitea.example.com/' \
  | ssh sshdock@sshdock.example.com config set gitea GITEA_ROOT_URL
openssl rand -hex 32 \
  | ssh sshdock@sshdock.example.com config set gitea GITEA_SECRET_KEY
openssl rand -hex 32 \
  | ssh sshdock@sshdock.example.com config set gitea GITEA_INTERNAL_TOKEN
ssh sshdock@sshdock.example.com config list gitea
sudo sshdock apps redeploy gitea
sudo sshdock domains attach gitea web gitea.example.com --port 18201
```

Open `https://gitea.example.com`, select SQLite3 and set the path to `/var/lib/gitea/data/gitea.db`, create the first administrator, and complete Gitea's official installation form. Keep the prefilled repository, domain, SSH port, HTTP port, base URL, and log paths. Public self-registration remains disabled after setup.

## Verify

Create a repository named `recipe-proof` in the Gitea web UI, add your SSH public key to that account, and push a real commit through Gitea's SSH service:

```bash
mkdir recipe-proof
cd recipe-proof
git init -b main
printf '%s\n' 'Gitea persistence through SSHDock' > README.md
git add README.md
git commit -m "Prove Gitea Git"
git remote add origin ssh://git@gitea.example.com:18222/acceptance/recipe-proof.git
git push -u origin main
cd ..
git clone ssh://git@gitea.example.com:18222/acceptance/recipe-proof.git recipe-proof-clone
cat recipe-proof-clone/README.md
curl -fsS https://gitea.example.com/api/healthz
sudo sshdock apps health gitea
sudo sshdock domains check gitea
sudo sshdock deployments list gitea
sudo sshdock events list gitea
```

The HTTPS health endpoint reports success, the public repository page shows the pushed commit, and the independent SSH clone contains the committed file.

## Operate

```bash
sudo sshdock logs gitea web --tail 100
ssh sshdock@sshdock.example.com apps exec gitea web -- gitea --version
sudo sshdock apps restart gitea
sudo sshdock apps health gitea
git -C recipe-proof-clone pull --ff-only
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://gitea.example.com/api/healthz
```

## Upgrade

Back up Gitea before every upgrade. Choose a supported exact rootless tag, resolve its current multi-platform digest from `docker.gitea.com`, review the release notes and upgrade guide, and update `compose.yml` plus the recorded pin above. Commit the pin change so Git selects the new deployment:

```bash
GITEA_DOMAIN=gitea.example.com \
GITEA_ROOT_URL=https://gitea.example.com/ \
GITEA_SECRET_KEY=local-only-secret-key \
GITEA_INTERNAL_TOKEN=local-only-internal-token \
  docker compose pull
git add compose.yml README.md
git commit -m "Upgrade Gitea recipe"
git push sshdock main
```

After the deployment succeeds, verify the repository page, pull or clone through port `18222`, and confirm the representative commit remains. Do not switch between rootless and rootful image families by changing only the image value; their volume layouts are not compatible.

## Cleanup

Ordinary SSHDock removal deletes app-owned containers, routes, and state while preserving Docker volumes:

```bash
sudo sshdock apps remove gitea --force
sudo docker volume ls --format '{{.Name}}' | grep '^sshdock_gitea_'
```

Only when you intentionally want to destroy repositories, the SQLite database, attachments, packages, secrets, and generated configuration, remove both volumes after removing the app:

```bash
sudo docker volume rm sshdock_gitea_gitea-data sshdock_gitea_gitea-config
```

## Persistence

`gitea-data` stores repositories, SQLite state, attachments, packages, and other application data at `/var/lib/gitea`. `gitea-config` stores the generated `app.ini` at `/etc/gitea`. Restart, redeploy, exact rootless-image upgrades, and ordinary app removal preserve both named volumes; SSHDock backup archives inventory these volumes but do not copy their contents.

## Limitations

This recipe proves the official first-run UI, one repository, SSH push and clone, HTTPS routing, health, logs, restart, exact-image upgrade, persistence, and cleanup. It does not provide managed upgrades, runners, email delivery, large-instance database tuning, application-consistent backups, object storage, high availability, or zero-downtime deployment.

## Security boundaries

The Gitea HTTP port is reachable only through host loopback and Caddy. TCP port `18222` is a separate Gitea application protocol that Caddy and SSHDock do not proxy or firewall; expose it through host and provider firewalls only when remote Git SSH is intended. Required domain and secret values live in SSHDock's encrypted config and stay redacted on normal operator surfaces. Keep registration disabled unless deliberately opened, maintain backups, review Gitea security releases, and protect administrator and SSH credentials. SSHDock's trusted-owner model does not sandbox malicious Compose workloads, repositories, hooks, actions, or packages.

# Next.js framework compatibility probe

## Purpose

This probe builds the unmodified official `create-next-app` starter inside the image and deploys its production server through SSHDock. It is point-in-time compatibility evidence, not editable application source or a framework tutorial.

Create a real user-owned Next.js application with the [official `create-next-app` workflow](https://nextjs.org/docs/app/getting-started/installation):

```bash
npx create-next-app@latest my-app --yes
```

## Prerequisites

- A working SSHDock server with a base domain configured.
- DNS for `*.example.com` pointing at the server.
- Your public key added to the `sshdock` operator account.
- Local `curl`, `git`, and `tar` commands.

Replace `example.com` below with the server's base domain.

## Topology

The root `compose.yml` builds one `web` service. The Dockerfile generates the official starter in a source stage, builds it in an intermediate stage, prunes development dependencies, and copies only production output and runtime dependencies into the final non-root image. Compose publishes port `3000` only on `127.0.0.1:18100`, checks the real starter page, and applies `restart: unless-stopped`.

## Pinned versions

- Generator and framework source: `create-next-app@16.2.10`
- Builder and runtime: `node:24.13.0-slim@sha256:4660b1ca8b28d6d1906fd644abe34b2ed81d15434d26d845ef0aced307cf4b6f`

The generator version and multi-platform Node manifest are immutable inputs. npm registry availability and transitive resolution can still change, so this is a tested compatibility claim for this SSHDock commit, not a promise of bit-for-bit rebuilds forever.

## Deploy

Until a release tag contains this probe, copy its three-file envelope from `main`:

```bash
mkdir nextjs
cd nextjs
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/nextjs
git init -b main
git add .
git commit -m "Deploy Next.js"
git remote add sshdock git@sshdock.example.com:nextjs.git
git push sshdock main
```

The first push creates the app, builds the generated production application, waits for its healthcheck, and creates the default route when the server has a base domain.

## Verify

Verify HTTPS and the official generated starter page:

```bash
curl -I http://nextjs.example.com
curl -fsS https://nextjs.example.com
sudo sshdock apps health nextjs
sudo sshdock domains check nextjs
sudo sshdock deployments list nextjs
sudo sshdock events list nextjs
```

HTTP redirects to HTTPS, and HTTPS returns the official `create-next-app` starter.

## Operate

Inspect bounded logs, restart the Compose service, and verify recovery:

```bash
sudo sshdock logs nextjs web --tail 100
sudo sshdock apps restart nextjs
sudo sshdock apps health nextjs
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://nextjs.example.com
```

To rebuild and deploy the same Git commit explicitly:

```bash
sudo sshdock apps redeploy nextjs
```

## Upgrade

Choose a tested `create-next-app` release and supported Node image, update the two Dockerfile arguments and recorded digest, then build the complete generated probe before pushing:

```bash
docker compose build --pull
docker compose up --wait
curl -fsS http://127.0.0.1:18100
git add Dockerfile README.md
git commit -m "Upgrade Next.js probe"
git push sshdock main
```

Review the generated starter and official Next.js release notes when changing versions. Do not commit the generated application tree or dependency manifests.

## Cleanup

Ordinary removal deletes SSHDock metadata, routes, and Compose containers:

```bash
sudo sshdock apps remove nextjs --force
```

## Persistence

The generated starter is stateless. The probe declares no named volumes, so no volume cleanup is required.

## Limitations

This probe proves the official generated App Router starter, production build and server, health, logs, restart, redeploy, and HTTPS routing. It does not demonstrate a database, object storage, background jobs, zero-downtime deployment, or platform-specific adapters.

## Security boundaries

The public HTTP port binds to IPv4 loopback so Caddy remains the external HTTP and HTTPS entry point. The final container runs as the non-root `node` user. SSHDock accepts trusted-owner Compose files; it does not sandbox malicious images or application code. Host patching, firewall policy, Docker maintenance, and Caddy maintenance remain operator responsibilities.

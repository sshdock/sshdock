# Next.js framework quickstart

## Purpose

Deploy the official Next.js Docker standalone template through SSHDock's public Git SSH path. The example proves a production build, a standalone Node.js runtime, automatic HTTPS routing, Compose health, logs, restart behavior, and a copyable upgrade path.

The application source, styling, Dockerfile, and Next.js configuration come from Vercel's [`with-docker`](https://github.com/vercel/next.js/tree/153bf8ac5fa00888ef5fbb2b65cac12f0942a44f/examples/with-docker) template. SSHDock adds exact dependency pins, one Compose service, loopback-only publication, health and restart policy, and this operator guide.

## Prerequisites

- A working SSHDock server with a base domain configured.
- DNS for `*.example.com` pointing at the server.
- Your public key added to the `sshdock` operator account.
- Local `curl`, `git`, and `tar` commands.

Replace `example.com` in the commands below with your SSHDock base domain.

## Topology

The root `compose.yml` defines one `web` service. The upstream multi-stage Dockerfile builds standalone output and runs it as the non-root `node` user on port `3000`. Compose publishes that service only on `127.0.0.1:18100`, checks the real root page, and applies `restart: unless-stopped`. SSHDock can infer the single web target and route HTTPS through Caddy.

## Pinned versions

- Upstream template revision `153bf8ac5fa00888ef5fbb2b65cac12f0942a44f`
- Next.js `16.2.10`
- React and React DOM `19.2.7`
- Tailwind CSS `4.3.2`
- TypeScript `5.9.3`
- Node image `24.13.0-slim`

Direct dependencies are exact pins. `package-lock.json` keeps transitive npm dependencies reproducible, including an override to the patched PostCSS `8.5.10` release.

## Deploy

Until a release tag contains this quickstart, copy it explicitly from the `main` branch:

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

The first push creates the `nextjs` app, builds the production image, waits for the root page to become healthy, and creates the default route when the server has a base domain.

## Verify

Verify the public user surface from your machine:

```bash
curl -I http://nextjs.example.com
curl -fsS https://nextjs.example.com
```

Expected evidence:

- HTTP redirects to HTTPS.
- HTTPS contains `Welcome to Next.js on Docker` from the upstream template.

Verify SSHDock's view from the server:

```bash
sudo sshdock apps health nextjs
sudo sshdock domains check nextjs
sudo sshdock deployments list nextjs
sudo sshdock events list nextjs
```

## Operate

Inspect bounded logs, restart the application, and confirm the route recovers:

```bash
sudo sshdock logs nextjs web --tail 100
sudo sshdock apps restart nextjs
sudo sshdock apps health nextjs
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://nextjs.example.com
```

The Compose restart policy also restarts the service after Docker or the host restarts.

## Upgrade

Review the current supported releases in the official Next.js and React release notes. Then update the exact direct dependency pins and lockfile, build locally, and push the tested commit:

```bash
npm install --save-exact next@<version> react@<version> react-dom@<version>
npm run typecheck
npm run build
git add package.json package-lock.json
git commit -m "Upgrade Next.js"
git push sshdock main
```

When updating from the upstream Docker template, inspect its changes instead of replacing SSHDock's loopback port, healthcheck, or restart policy. Update the exact Node image tag when the template moves to a supported Node LTS patch.

## Cleanup

Ordinary removal deletes SSHDock metadata, routes, and Compose containers for this stateless example:

```bash
sudo sshdock apps remove nextjs --force
sudo sshdock apps list
```

No Docker volume cleanup is needed because this quickstart declares no named volumes.

## Persistence

The application is stateless. Source comes from Git, and the image is rebuilt from the pushed commit. Add a named volume only when an application has data that must outlive a container.

## Limitations

This quickstart proves the official Next.js Node.js server and App Router Docker shape. It does not demonstrate a database, external object storage, background jobs, image-optimization storage, zero-downtime deployment, or platform-specific Next.js adapters.

## Security boundaries

The public HTTP port binds to IPv4 loopback so Caddy is the external HTTP and HTTPS entry point. The container runs as a non-root user. SSHDock accepts trusted-owner Compose files; it does not sandbox malicious images or application code. Host patching, firewall policy, Docker maintenance, and Caddy maintenance remain operator responsibilities.

# NestJS framework compatibility probe

## Purpose

This probe builds the unmodified official Nest CLI TypeScript starter inside the image and deploys its production API through SSHDock. It is point-in-time compatibility evidence, not editable application source or a framework tutorial.

Create a real user-owned application with the [official NestJS first-steps workflow](https://docs.nestjs.com/first-steps):

```bash
npx @nestjs/cli@latest new my-app --package-manager npm --strict
```

## Prerequisites

- A working SSHDock server with a base domain configured.
- DNS for `*.example.com` pointing at the server.
- Your public key added to the `sshdock` operator account.
- Local `curl`, `git`, and `tar` commands.

Replace `example.com` below with the server's base domain.

## Topology

The root `compose.yml` builds one `web` service. The Dockerfile runs the official generator in a source stage, builds and prunes development dependencies in intermediate stages, and copies only compiled output and production dependencies into the final non-root image. Compose publishes port `3000` only on `127.0.0.1:18101`, checks the official generated `GET /` response, and applies `restart: unless-stopped`.

## Pinned versions

- Generator and framework source: `@nestjs/cli@11.0.24`
- Builder and runtime: `node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd`

The generator version and multi-platform Node manifest are immutable inputs. npm registry availability and transitive resolution can still change, so this is a tested compatibility claim for this SSHDock commit, not a promise of bit-for-bit rebuilds forever.

## Deploy

Until a release tag contains this probe, copy its three-file envelope from `main`:

```bash
mkdir nestjs
cd nestjs
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/nestjs
git init -b main
git add .
git commit -m "Deploy NestJS"
git remote add sshdock git@sshdock.example.com:nestjs.git
git push sshdock main
```

The first push creates the app, builds the generated production API, waits for its healthcheck, and creates the default route when the server has a base domain.

## Verify

```bash
curl -I http://nestjs.example.com
curl -fsS https://nestjs.example.com
sudo sshdock apps health nestjs
sudo sshdock domains check nestjs
sudo sshdock deployments list nestjs
sudo sshdock events list nestjs
```

The HTTPS response is the official starter result:

```text
Hello World!
```

## Operate

```bash
sudo sshdock logs nestjs web --tail 100
sudo sshdock apps restart nestjs
sudo sshdock apps health nestjs
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://nestjs.example.com
```

To rebuild and deploy the same Git commit explicitly:

```bash
sudo sshdock apps redeploy nestjs
```

## Upgrade

Choose a tested Nest CLI release and supported Node image, update the two Dockerfile arguments and recorded digest, then build the complete generated probe before pushing:

```bash
docker compose build --pull
docker compose up --wait
curl -fsS http://127.0.0.1:18101
git add Dockerfile README.md
git commit -m "Upgrade NestJS probe"
git push sshdock main
```

Review the generated starter and official NestJS release notes when changing versions. Do not commit the generated application tree, dependency manifests, lockfiles, caches, or build output.

## Cleanup

```bash
sudo sshdock apps remove nestjs --force
```

## Persistence

The generated starter is stateless. The probe declares no volumes, so no volume cleanup is required.

## Limitations

This probe proves the official generated API, production build and runtime, health, logs, restart, redeploy, and HTTPS routing. It does not demonstrate a database, authentication, background jobs, zero-downtime deployment, or framework development.

## Security boundaries

The public HTTP port binds to IPv4 loopback so Caddy remains the external HTTP and HTTPS entry point. The final container runs as the non-root `node` user. SSHDock accepts trusted-owner Compose files; it does not sandbox malicious images or application code. Host patching, firewall policy, Docker maintenance, and Caddy maintenance remain operator responsibilities.

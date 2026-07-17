# NestJS on SSHDock

## Purpose

Deploy Nest's smallest official TypeScript starter as a production API without changing its generated application, tests, dependency manifest, or lockfile.

## Prerequisites

- An SSHDock server with a base domain and deploy key configured
- Git and curl on the local machine
- Node.js and npm only when verifying or regenerating the starter locally

## Topology

The root Compose file builds one `web` service, publishes its HTTP port only on `127.0.0.1:18101`, checks the official starter's `GET /` response, and restarts the container after a host reboot. SSHDock routes HTTPS traffic to that loopback-bound port.

The image uses separate build and runtime stages. The runtime contains production dependencies and compiled JavaScript, runs as the image's non-root `node` user, and starts with `node dist/main`.

## Pinned versions

- Generator: `@nestjs/cli@11.0.24`
- Runtime and build image: `node:24.18.0-alpine3.24`
- Generated dependency tree: `package-lock.json`

The starter was generated with:

```bash
npx --yes @nestjs/cli@11.0.24 new nestjs --package-manager npm --strict
```

The generated `.git` directory is not part of this example, and the generated Nest README is replaced by this SSHDock operations README. Every other generated file is preserved as created and registered in the shared contract harness.

## Deploy

Until a release tag contains this quickstart, copy it explicitly from `main`:

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

SSHDock builds the image, waits for the Compose healthcheck, and creates the default `https://nestjs.example.com` route.

## Verify

```bash
curl -I http://nestjs.example.com
curl -fsS https://nestjs.example.com
sudo sshdock apps health nestjs
```

The HTTPS response is the official starter result:

```text
Hello World!
```

## Operate

```bash
sudo sshdock logs nestjs web --tail 100
sudo sshdock apps restart nestjs
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://nestjs.example.com
```

Use `sudo sshdock apps redeploy nestjs` instead when you need a new deployment attempt for the current Git `main`.

## Upgrade

Regenerate the official starter as one unit with the desired exact CLI version in a temporary directory. Replace the generated starter files together, retain the four SSHDock envelope files (`.dockerignore`, `Dockerfile`, `compose.yml`, and this README), then rebuild and push:

```bash
npx --yes @nestjs/cli@<version> new nestjs --package-manager npm --strict
npm test -- --runInBand --no-watchman
npm run test:e2e -- --runInBand --no-watchman
npm run build
git add .
git commit -m "Upgrade NestJS"
git push sshdock main
```

Update the recorded generator and Node image versions with the regenerated lockfile. Do not hand-edit the generated application or dependency manifest during the upgrade.

## Cleanup

```bash
sudo sshdock apps remove nestjs --force
```

## Persistence

This official starter is stateless. It declares no volumes, database, or external service.

## Limitations

The quickstart proves NestJS production build, HTTP serving, health, logs, restart, redeploy, and HTTPS routing. It intentionally adds no API features, database, authentication, or framework tutorial.

## Security boundaries

The application port is bound to IPv4 loopback, so public access is expected through SSHDock's Caddy route. The container runs as the non-root `node` user. The example contains no secrets; add required values through SSHDock config and reference only the needed keys from Compose.

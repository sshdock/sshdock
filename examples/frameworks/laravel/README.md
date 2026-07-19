# Laravel framework compatibility probe

## Purpose

This probe generates the unmodified official Laravel application skeleton inside the image and serves its production output with FrankenPHP through SSHDock. It is point-in-time compatibility evidence, not editable starter source or a Laravel tutorial.

Create a real user-owned application with the [official Laravel installation workflow](https://laravel.com/docs/13.x/installation):

```bash
laravel new example-app
```

The equivalent Composer package bootstrap used by this probe is `composer create-project laravel/laravel example-app` with an exact skeleton version.

## Prerequisites

- A working SSHDock server with a base domain and deploy key configured.
- DNS for `*.example.com` pointing at the server.
- Local `curl`, `git`, `openssl`, and `tar` commands.

Replace `example.com` below with the server's base domain.

## Topology

The root `compose.yml` builds one `web` service. Intermediate stages generate `laravel/laravel:13.8.0`, install production Composer dependencies, and build Vite assets. The final image contains only the production application and runtime dependencies, runs FrankenPHP as `www-data`, and never bootstraps the application at container startup.

Compose publishes port `8080` only on `127.0.0.1:18102`, checks Laravel's real `/up` route, applies `restart: unless-stopped`, and mounts named `laravel_storage` at `/app/storage` for writable persistence. `APP_URL` and `ASSET_URL` default to the HTTPS route.

## Pinned versions

- Official skeleton: `laravel/laravel:13.8.0`
- Skeleton source tag: [`v13.8.0`](https://github.com/laravel/laravel/releases/tag/v13.8.0)
- Generator: `composer:2.10.2@sha256:5946476338742b200bb9ff88f8be56275ddae4b3949c72305cb0dbf10cfcb760`
- Asset builder: `node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd`
- PHP runtime: `dunglas/frankenphp:1.12.3-php8.5-alpine@sha256:19eda5f22186afeda3aaa70f103a7019bbcff57980da8069f7861c1034aa81ae`

The skeleton version and multi-platform image manifests are immutable inputs. Composer and npm registry availability can still change, so this is a tested compatibility claim for this SSHDock commit, not a promise of bit-for-bit rebuilds forever.

## Deploy

Until a release tag contains this probe, copy its three-file envelope from `main`:

```bash
mkdir laravel
cd laravel
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/laravel
git init -b main
git add .
git commit -m "Deploy Laravel"
git remote add sshdock git@sshdock.example.com:laravel.git
git push sshdock main
```

The accepted push creates the app but stops before build because Compose requires `APP_KEY`. Recover through the restricted config surface, confirm the value is redacted, redeploy current remote `main`, and attach the conventional route:

```bash
printf 'base64:%s' "$(openssl rand -base64 32)" \
  | ssh sshdock@sshdock.example.com config set laravel APP_KEY
ssh sshdock@sshdock.example.com config list laravel
sudo sshdock apps redeploy laravel
sudo sshdock domains attach laravel web laravel.example.com --port 18102
```

Set `APP_URL` through the same config surface before redeploying when the hostname differs. Automatic first-route creation belongs to a successful Git-receive deployment, so required-config recovery attaches the route explicitly.

## Verify

```bash
curl -I http://laravel.example.com
curl -fsS https://laravel.example.com
curl -fsS https://laravel.example.com/up
sudo sshdock apps health laravel
sudo sshdock domains check laravel
sudo sshdock deployments list laravel
sudo sshdock events list laravel
```

HTTP redirects to HTTPS, HTTPS returns the official generated welcome page, and `/up` reports healthy.

## Operate

```bash
sudo sshdock logs laravel web --tail 100
ssh sshdock@sshdock.example.com apps exec laravel web -- php artisan about
ssh sshdock@sshdock.example.com apps run laravel web -- php artisan migrate --force
sudo sshdock apps restart laravel
sudo sshdock apps health laravel
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://laravel.example.com
sudo sshdock apps redeploy laravel
```

The named volume preserves storage and the SQLite database across restart and redeploy.

## Upgrade

Choose a tested `laravel/laravel` release and supported Composer, Node, and FrankenPHP images. Update the four Dockerfile arguments and recorded digests, then build the complete generated probe before pushing:

```bash
APP_KEY='base64:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=' docker compose build --pull
APP_KEY='base64:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=' docker compose up --wait
curl -fsS http://127.0.0.1:18102
git add Dockerfile README.md
git commit -m "Upgrade Laravel probe"
git push sshdock main
```

Review the generated skeleton and official Laravel upgrade guide when changing versions. Do not commit generated application source, manifests, lockfiles, caches, or build output.

## Cleanup

```bash
sudo sshdock apps remove laravel --force
```

Ordinary app removal preserves `laravel_storage`. Delete the volume separately through server administration only when its sessions, logs, cache, and SQLite data are intentionally disposable.

## Persistence

`laravel_storage` persists Laravel's writable storage and SQLite database across restart, redeploy, and app removal. The probe adds no external database service.

## Limitations

This probe proves the official generated welcome page, production serving, required config recovery, HTTPS URLs, health, logs, restricted exec and one-off commands, persistence, restart, redeploy, and routing. It adds no starter kit, application feature, queue worker, scheduler, Redis, or external database.

## Security boundaries

The application port binds to IPv4 loopback so Caddy remains the public HTTP and HTTPS entry point. The final container runs as non-root `www-data`, and `APP_KEY` enters only through SSHDock config with redacted listings. SSHDock accepts trusted-owner Compose code; it does not sandbox malicious images or application code.

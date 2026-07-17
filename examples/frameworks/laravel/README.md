# Laravel on SSHDock

## Purpose

Deploy Laravel's smallest official application skeleton through a production FrankenPHP runtime without changing the starter application, tests, or dependency manifests.

## Prerequisites

- An SSHDock server with a base domain and deploy key configured
- Git, curl, and OpenSSL on the local machine
- Docker only when verifying or regenerating the starter locally

## Topology

The root Compose file builds one `web` service, publishes FrankenPHP only on `127.0.0.1:18102`, checks Laravel's built-in `/up` health route, and restarts the container after a host reboot. SSHDock routes HTTPS traffic to that loopback-bound port.

The production image builds Vite assets with Node, installs locked Composer dependencies, enables PHP production settings and SQLite, and runs FrankenPHP as `www-data`. A named volume mounted at `/app/storage` preserves logs, file sessions, cache data, and the optional SQLite database across redeploys.

## Pinned versions

- Official skeleton: `laravel/laravel:v13.8.0`
- Source commit: `e196bfdfc96903f2e10219749fcbca7c0aefe99f`
- PHP web runtime: `dunglas/frankenphp:1.12.3-php8.5-alpine`
- Composer image: `composer:2.10.2`
- Asset image: `node:24.18.0-alpine3.24`
- Generated PHP dependency tree: `composer.lock`
- Generated JavaScript dependency tree: `package-lock.json`

The starter and lockfiles were generated with:

```bash
composer create-project --no-interaction --prefer-dist laravel/laravel laravel v13.8.0
cd laravel
npm install --package-lock-only --ignore-scripts
```

The generated `.env`, `vendor`, `node_modules`, Composer cache files, migrated SQLite file, and upstream README are not part of this quickstart. Starter-owned application files and both dependency manifests remain unchanged; only `.dockerignore`, `Dockerfile`, `compose.yml`, and this operations README form the SSHDock envelope.

## Deploy

Until a release tag contains this quickstart, copy it explicitly from `main`:

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

The first accepted push creates `laravel` but deployment stops before startup because `APP_KEY` is required. Store a fresh application key through the restricted SSH config surface, inspect the redacted key list, redeploy current remote `main`, then attach the conventional domain explicitly:

```bash
printf 'base64:%s' "$(openssl rand -base64 32)" \
  | ssh sshdock@sshdock.example.com config set laravel APP_KEY
ssh sshdock@sshdock.example.com config list laravel
sudo sshdock apps redeploy laravel
sudo sshdock domains attach laravel web laravel.example.com --port 18102
```

The redeploy builds the production image and waits for `/up`. Automatic first-route creation belongs to a successful Git-receive deployment, so recovery from required config uses the supported manual attach command before verifying `https://laravel.example.com`.

The Compose defaults make both the application URL and generated asset URLs use `https://laravel.example.com`. If you deploy under a different hostname, store that HTTPS URL as `APP_URL` before redeploying.

## Verify

```bash
curl -I http://laravel.example.com
curl -fsS https://laravel.example.com
curl -fsS https://laravel.example.com/up
sudo sshdock apps health laravel
```

The HTTPS response is Laravel's official welcome page. The health endpoint returns a successful empty response.

## Operate

```bash
sudo sshdock logs laravel web --tail 100
sudo sshdock apps restart laravel
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://laravel.example.com
ssh sshdock@sshdock.example.com apps exec laravel web -- php artisan about
ssh sshdock@sshdock.example.com apps run laravel web -- php artisan migrate --force
```

Use `sudo sshdock apps redeploy laravel` when you need another deployment attempt for current remote `main`, such as after later config changes. Existing routes remain attached.

## Upgrade

Generate the desired exact `laravel/laravel` version in a temporary directory. Replace the starter as one unit, retain the four SSHDock envelope files, regenerate both lockfiles, run the starter tests and production image, and push the replacement commit:

```bash
composer create-project --no-interaction --prefer-dist laravel/laravel laravel <version>
cd laravel
npm install --package-lock-only --ignore-scripts
php artisan test
npm ci
npm run build
docker build .
git add .
git commit -m "Upgrade Laravel"
git push sshdock main
```

Update the recorded source commit and image versions. Do not hand-edit starter application files or dependency manifests during the upgrade.

## Cleanup

```bash
sudo sshdock apps remove laravel --force
```

Ordinary app removal preserves the Docker volume. Delete that volume separately through server administration only when its sessions, logs, cache data, and SQLite data are intentionally disposable.

## Persistence

The `laravel_storage` named volume persists Laravel's writable storage and SQLite database across restart, redeploy, and app removal. The official starter does not add an external database service.

## Limitations

The quickstart proves the official Laravel welcome page, production serving, required secret config, health, logs, restricted exec and one-off commands, persistence, restart, redeploy, and HTTPS routing. It intentionally adds no authentication starter kit, application feature, queue worker, scheduler, Redis, or external database.

## Security boundaries

The application port is bound to IPv4 loopback, so public access is expected through SSHDock's Caddy route. FrankenPHP runs as `www-data`, and `APP_KEY` enters only through SSHDock config. Normal config listings stay redacted. The application remains trusted-owner Compose code; the example does not claim workload sandboxing.

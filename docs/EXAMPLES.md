# SSHDock Examples

The maintained public suite is organized around four questions: which framework to deploy, which recognizable software to run, how to connect databases safely, and how to exercise SSHDock-specific workflows.

```text
git push -> first app creation -> Compose deploy -> optional route -> SSH dashboard visibility
```

Each registered example is meant to be copied into a new local directory and pushed to a real SSHDock server. Replace `example.com` with the base domain configured by `sudo sshdock server domain set <domain>`. Release-tagged URLs are used only after a release contains the referenced files; pre-release examples name the `main` branch explicitly.

## Framework quickstarts

Framework quickstarts teach the SSHDock integration path, not the framework itself. Each one uses a production runtime, one root Compose file, a health signal, restart behavior, Git-push deployment, HTTPS verification, day-two inspection, upgrade guidance, and cleanup.

### Next.js

Path: [`examples/frameworks/nextjs`](../examples/frameworks/nextjs/README.md)

The Next.js compatibility probe generates the unmodified official starter during its pinned image build. The repository keeps only the three-file SSHDock deployment envelope: a multi-stage Dockerfile, one loopback-bound Compose service with health and restart behavior, and an operations README.

Until a release tag contains the probe, copy it explicitly from `main`:

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

Verify and operate the deployed application:

```bash
curl -I http://nextjs.example.com
curl -fsS https://nextjs.example.com
sudo sshdock apps health nextjs
sudo sshdock logs nextjs web --tail 100
sudo sshdock apps restart nextjs
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://nextjs.example.com
```

Upgrade the pinned generator and Node image inputs, verify the generated production application, and push the new commit:

```bash
docker compose build --pull
docker compose up --wait
curl -fsS http://127.0.0.1:18100
git add Dockerfile README.md
git commit -m "Upgrade Next.js probe"
git push sshdock main
```

Clean up the stateless example:

```bash
sudo sshdock apps remove nextjs --force
```

See the probe README for exact generator and image provenance, topology, expected evidence, persistence, limitations, and security boundaries.

### NestJS

Path: [`examples/frameworks/nestjs`](../examples/frameworks/nestjs/README.md)

The NestJS compatibility probe generates the unmodified official starter during its pinned image build. The repository keeps only the three-file SSHDock deployment envelope: a multi-stage Dockerfile, one loopback-bound Compose service with health and restart behavior, and an operations README.

Until a release tag contains the probe, copy it explicitly from `main`:

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

Verify and operate the API:

```bash
curl -I http://nestjs.example.com
curl -fsS https://nestjs.example.com
sudo sshdock apps health nestjs
sudo sshdock logs nestjs web --tail 100
sudo sshdock apps restart nestjs
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://nestjs.example.com
```

Upgrade the pinned generator and Node image inputs, verify the generated production API, and push only the three-file envelope. Generated source, manifests, lockfiles, caches, and build output stay outside the repository.

Clean up the stateless example:

```bash
sudo sshdock apps remove nestjs --force
```

See the probe README for exact generator and image provenance, topology, expected evidence, persistence, limitations, and security boundaries.

### Laravel

Path: [`examples/frameworks/laravel`](../examples/frameworks/laravel/README.md)

The Laravel compatibility probe generates the unmodified official skeleton during its pinned image build. The repository keeps only a three-file SSHDock envelope: a multi-stage FrankenPHP Dockerfile, one loopback-bound Compose service with required `APP_KEY`, HTTPS URLs, health, restart behavior and named storage, and an operations README.

Until a release tag contains the probe, copy it explicitly from `main`:

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

The accepted push creates the app and stops safely at the required `APP_KEY`. Store the key, redeploy current remote `main`, and attach the conventional domain explicitly because automatic first-route creation belongs to a successful Git-receive deployment:

```bash
printf 'base64:%s' "$(openssl rand -base64 32)" \
  | ssh sshdock@sshdock.example.com config set laravel APP_KEY
sudo sshdock apps redeploy laravel
sudo sshdock domains attach laravel web laravel.example.com --port 18102
```

Verify and operate the application:

```bash
curl -I http://laravel.example.com
curl -fsS https://laravel.example.com
sudo sshdock apps health laravel
sudo sshdock logs laravel web --tail 100
sudo sshdock apps restart laravel
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://laravel.example.com
```

Upgrade by changing the exact skeleton and image inputs in the Dockerfile, verifying the generated production image, and pushing only the three-file envelope. Generated source, manifests, lockfiles, caches, and build output stay outside the repository.

Clean up the app while preserving its named storage volume:

```bash
sudo sshdock apps remove laravel --force
```

See the probe README for exact provenance, config recovery, topology, persistence, restricted operations, upgrade boundaries, limitations, and security boundaries.

### Gin

Path: [`examples/frameworks/gin`](../examples/frameworks/gin/README.md)

The Gin compatibility probe checks out the official `gin-gonic/examples/basic` source at an exact commit during its pinned image build. The repository keeps only the three-file SSHDock deployment envelope: a multi-stage Dockerfile, one loopback-bound Compose service with health and restart behavior, and an operations README.

Until a release tag contains the probe, copy it explicitly from `main`:

```bash
mkdir gin
cd gin
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/gin
git init -b main
git add .
git commit -m "Deploy Gin"
git remote add sshdock git@sshdock.example.com:gin.git
git push sshdock main
```

Verify and operate the compiled service:

```bash
curl -I http://gin.example.com/ping
curl -fsS https://gin.example.com/ping
sudo sshdock apps health gin
sudo sshdock logs gin web --tail 100
sudo sshdock apps restart gin
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://gin.example.com/ping
```

Upgrade the pinned source revision and Go and Alpine image inputs, verify the official service, and push only the three-file envelope. Upstream source, module manifests, caches, and build output stay outside the repository.

Clean up the stateless example:

```bash
sudo sshdock apps remove gin --force
```

See the probe README for exact source and image provenance, topology, expected evidence, transient state, limitations, and security boundaries.

### Phoenix LiveView

Path: [`examples/frameworks/phoenix`](../examples/frameworks/phoenix/README.md)

The Phoenix LiveView compatibility probe generates an official one-field LiveView resource during its pinned image build. The repository keeps only the three-file SSHDock deployment envelope: a multi-stage Dockerfile, one loopback-bound Compose service with health, restart behavior, and named SQLite persistence, and an operations README.

Until a release tag contains the probe, copy it explicitly from `main`:

```bash
mkdir phoenix
cd phoenix
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/phoenix
git init -b main
git add .
git commit -m "Deploy Phoenix LiveView"
git remote add sshdock git@sshdock.example.com:phoenix.git
git push sshdock main
```

Store the required Phoenix signing secret, redeploy the accepted commit, and attach the conventional domain after the initial config-gated deployment:

```bash
openssl rand -base64 48 \
  | ssh sshdock@sshdock.example.com config set phoenix SECRET_KEY_BASE
printf '%s' 'phoenix.example.com' \
  | ssh sshdock@sshdock.example.com config set phoenix PHX_HOST
sudo sshdock apps redeploy phoenix
sudo sshdock domains attach phoenix web phoenix.example.com --port 18104
```

Verify and operate the generated LiveView:

```bash
curl -I http://phoenix.example.com/items
curl -fsS https://phoenix.example.com/items
sudo sshdock apps health phoenix
sudo sshdock logs phoenix web --tail 100
sudo sshdock apps restart phoenix
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://phoenix.example.com/items
```

Keep the browser tab open across the supported restart to observe LiveView reconnect, then submit another generated Item through the secure WebSocket. Upgrade the pinned generator, Elixir/Erlang builder, and Alpine runtime together; verify the fresh generated production release before pushing only the three-file envelope.

Clean up the app while preserving its named SQLite volume:

```bash
sudo sshdock apps remove phoenix --force
```

See the probe README for exact provenance, required config, LiveView interaction and reconnect evidence, topology, persistence, limitations, and security boundaries.

## Software recipes

Software recipes run recognizable upstream applications while preserving their supported architecture where it fits SSHDock's v0 boundary. A registered recipe pins versions, stores non-public values through SSHDock config, declares persistence, proves the real first-run surface, and separates ordinary removal from destructive volume cleanup.

### WordPress

Path: [`examples/software/wordpress`](../examples/software/wordpress)

The WordPress recipe runs exact official WordPress Apache and MariaDB images in the smallest viable two-service topology. It keeps the database private, stores database values through SSHDock config, publishes one loopback-bound HTTP port, gates the public service on database readiness, and preserves site files, uploads, and database content in named volumes.

Follow the recipe README for first-run setup, representative-content verification, routine operation, a Git-selected exact-image update, ordinary removal, destructive volume cleanup, persistence, limitations, and security boundaries. The existing WordPress Lite fixture below remains a feature demonstration until the canonical-suite cleanup ticket removes old examples.

### Gitea

Path: [`examples/software/gitea`](../examples/software/gitea)

The Gitea recipe runs the exact official rootless image with its supported SQLite topology. It stores repositories, the database, and configuration in named volumes, keeps the web service behind a loopback-bound Caddy route, and publishes the built-in Git SSH service on the documented non-conflicting host port `18222`.

Follow the recipe README for required SSHDock config, first-run setup, repository creation, real SSH Git push and clone verification, a Git-selected exact-image update, routine operation, ordinary removal, destructive volume cleanup, port ownership, persistence, limitations, and security boundaries.

### n8n

Path: [`examples/software/n8n`](../examples/software/n8n)

The n8n recipe runs one exact official image with n8n's default SQLite topology. It keeps the editor and published webhook endpoints behind a loopback-bound Caddy route, stores the encryption key through SSHDock config, and preserves the upstream `.n8n` state directory in a named volume.

Follow the recipe README for first-run setup, a persistent published webhook workflow, exact-image upgrades, routine operation, ordinary removal, destructive volume cleanup, scheduling ownership, persistence, limitations, and security boundaries.

## Database examples

Database examples teach explicit operator-owned connectivity patterns. A registered example must keep databases off the public internet by default, state who owns networks and credentials, and verify access through the intended protocol rather than container health alone.

No database example is registered in the maintained contract yet. The API and PostgreSQL fixture below remains a local multi-service demonstration.

## Feature labs

Feature labs reuse registered framework, software, or database examples to teach SSHDock config, Git recovery, restricted operations, routing, inspection, and backup boundaries. They do not introduce another toy application for each command.

No feature lab is registered in the maintained contract yet. The existing fixtures remain available while their workflows are moved onto verified public examples.

## Existing feature demonstrations

The examples below predate the maintained four-category contract. They continue to support the current stable release and local regression harness until their replacement slices pass the same contract and real-host acceptance.

## User-Story Matrix

| User story | Example | What it proves |
| --- | --- | --- |
| Minimal public app | `examples/static-site` | image service, loopback port, automatic default HTTPS route |
| Build-based service | `examples/build-service` | Dockerfile build path and app logs |
| Config-backed app | `examples/config-app` | missing required config failure, SSH config set/list/get, redaction |
| Worker-only app | `examples/worker-only` | background service with no public route |
| Web + worker + Redis | `examples/web-worker-redis` | public web service with private worker and Redis services |
| API + Postgres | `examples/api-postgres` | routed API, private database, database healthcheck, named volume |
| Stateful volume app | `examples/stateful-counter` | built service with persistent named-volume state across redeploy |
| Bad deploy and rollback | `examples/rollback-lab` | failed deploy evidence and rollback recovery |
| WordPress-style app | `examples/wordpress-lite` | common stateful web + database Compose shape |
| Custom domain/manual route | use any routed example | explicit `sshdock domains attach` when auto-routing is not enough |

Docker Compose is the current runtime engine for these examples. SSHDock may explore k3s later as an advanced runtime engine, but that is a direction, not a promise. The examples are written to keep the user-facing contract Compose-first and SSH-native. See [`RUNTIME_ENGINES.md`](RUNTIME_ENGINES.md).

For root-file selection, Compose authority, project isolation, and the external-file boundary, see [`COMPOSE_SUPPORT.md`](COMPOSE_SUPPORT.md).

## Minimal Static Site

Path:

```text
examples/static-site
```

This example proves the smallest web-app path: one `web` service, one loopback-published port, automatic route inference, and static content served through Caddy.

Deploy:

```bash
mkdir static-site
cd static-site
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site/README.md
mkdir public
curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site/public/index.html
git init -b main
git add .
git commit -m "Deploy static site"
git remote add sshdock git@sshdock.example.com:static-site.git
git push sshdock main
```

Verify on the server:

```bash
sudo sshdock apps list
sudo sshdock domains list static-site
sudo sshdock events list static-site
sudo sshdock logs static-site web
ssh -T sshdock@sshdock.example.com
```

Verify from your machine:

```bash
curl -I http://static-site.example.com
curl -fsS https://static-site.example.com
```

Expected evidence:

- `apps list` shows `static-site healthy local`.
- `domains list static-site` includes `static-site.example.com`, service `web`, and port `18080`.
- `events list static-site` includes `deploy.succeeded`, `route.auto_attached`, and `router.reloaded`.
- HTTP returns a redirect to HTTPS.
- HTTPS returns the page containing `SSHDock static site OK`.
- `ssh -T sshdock@sshdock.example.com` shows the app, route, release, deployment, events, and logs.

Clean up:

On the SSHDock server:

```bash
sudo sshdock apps remove static-site --force
sudo sshdock apps list
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_static-site-' || true
```

On your machine, remove the scratch copy:

```bash
cd ..
rm -rf static-site
```

Expected cleanup evidence:

- `apps list` no longer shows `static-site`.
- No Docker containers named `sshdock_static-site-*` remain.
- The local `static-site` directory is removed from your machine.

## Build Service

Path:

```text
examples/build-service
```

This example proves the Compose build path: one `web` service built from a local Dockerfile, one loopback-published port, automatic route inference, and app logs served through SSHDock.

Deploy:

```bash
mkdir build-service
cd build-service
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/Dockerfile
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/server.py
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/README.md
git init -b main
git add .
git commit -m "Deploy build service"
git remote add sshdock git@sshdock.example.com:build-service.git
git push sshdock main
```

Verify on the server:

```bash
sudo sshdock apps list
sudo sshdock domains list build-service
sudo sshdock events list build-service
sudo sshdock logs build-service web
ssh -T sshdock@sshdock.example.com
```

Verify from your machine:

```bash
curl -I http://build-service.example.com
curl -fsS https://build-service.example.com
```

Expected evidence:

- `apps list` shows `build-service healthy local`.
- `domains list build-service` includes `build-service.example.com`, service `web`, and port `18083`.
- `events list build-service` includes `deploy.succeeded`, `route.auto_attached`, and `router.reloaded`.
- HTTP returns a redirect to HTTPS.
- HTTPS returns `SSHDock build service OK`.
- `ssh -T sshdock@sshdock.example.com` shows the app, route, release, deployment, events, and logs.

Clean up:

On the SSHDock server:

```bash
sudo sshdock apps remove build-service --force
sudo sshdock apps list
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_build-service-' || true
```

On your machine, remove the scratch copy:

```bash
cd ..
rm -rf build-service
```

Expected cleanup evidence:

- `apps list` no longer shows `build-service`.
- No Docker containers named `sshdock_build-service-*` remain.
- The local `build-service` directory is removed from your machine.

## Config App

Path:

```text
examples/config-app
```

This example proves app config without committing values: Compose requires `APP_MESSAGE` with native interpolation, the first push fails before containers start, and the second push succeeds after the value is stored over SSH.

The app renders `APP_MESSAGE` publicly, so use a non-secret demo value for this example. The same config feature can store secrets, but applications should not return real secrets in HTTP responses.

Deploy:

```bash
mkdir config-app
cd config-app
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/Dockerfile
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/server.py
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/README.md
git init -b main
git add .
git commit -m "Deploy config app"
git remote add sshdock git@sshdock.example.com:config-app.git
git push sshdock main
```

Expected first-push evidence:

- The remote creates `config-app`.
- Deploy fails with `required variable APP_MESSAGE is missing a value`.
- Docker Compose does not start.

Set the missing value:

```bash
printf '%s\n' 'Hello from SSHDock config' | ssh sshdock@sshdock.example.com config set config-app APP_MESSAGE
ssh sshdock@sshdock.example.com config list config-app
ssh sshdock@sshdock.example.com config get config-app APP_MESSAGE
```

Expected config evidence:

- `config set` confirms the key was stored without echoing the value.
- `config list config-app` shows `APP_MESSAGE` as `<redacted>`.
- `config get config-app APP_MESSAGE` prints `Hello from SSHDock config`.

Create a new commit so Git runs the receive hook, then push again:

```bash
git commit --allow-empty -m "Deploy with config"
git push sshdock main
```

Verify on the server:

```bash
sudo sshdock apps list
sudo sshdock domains list config-app
sudo sshdock events list config-app
sudo sshdock logs config-app web
ssh -T sshdock@sshdock.example.com
```

Verify from your machine:

```bash
curl -I http://config-app.example.com
curl -fsS https://config-app.example.com
```

Expected deploy evidence:

- `apps list` shows `config-app healthy local`.
- `domains list config-app` includes `config-app.example.com`, service `web`, and port `18082`.
- `events list config-app` includes the initial failed deploy, the later `deploy.succeeded`, `route.auto_attached`, and `router.reloaded`.
- HTTP returns a redirect to HTTPS.
- HTTPS returns `SSHDock config example: Hello from SSHDock config`.
- `ssh -T sshdock@sshdock.example.com` shows the app, route, release, deployment, events, logs, and redacted config.

Clean up:

On the SSHDock server:

```bash
sudo sshdock apps remove config-app --force
sudo sshdock apps list
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_config-app-' || true
```

On your machine, remove the scratch copy:

```bash
cd ..
rm -rf config-app
```

Expected cleanup evidence:

- `apps list` no longer shows `config-app`.
- No Docker containers named `sshdock_config-app-*` remain.
- The local `config-app` directory is removed from your machine.

## Worker Only

Path:

```text
examples/worker-only
```

This example proves a background app with no public route.

Deploy:

```bash
mkdir worker-only
cd worker-only
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/worker-only/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/worker-only/Dockerfile
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/worker-only/worker.sh
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/worker-only/README.md
git init -b main
git add .
git commit -m "Deploy worker only"
git remote add sshdock git@sshdock.example.com:worker-only.git
git push sshdock main
```

Verify:

```bash
sudo sshdock apps list
sudo sshdock domains list worker-only
sudo sshdock logs worker-only worker
ssh -T sshdock@sshdock.example.com
```

Expected evidence:

- `apps list` shows `worker-only healthy local`.
- `domains list worker-only` has no public route.
- `logs worker-only worker` includes `SSHDock worker-only example tick`.
- The dashboard shows the worker service.

Clean up:

```bash
sudo sshdock apps remove worker-only --force
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_worker-only-' || true
```

No Docker volumes need to be removed.

## Web Worker Redis

Path:

```text
examples/web-worker-redis
```

This example proves a common app topology: public web service, background worker, and private Redis service.

Deploy:

```bash
mkdir web-worker-redis
cd web-worker-redis
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/web-worker-redis/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/web-worker-redis/Dockerfile.worker
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/web-worker-redis/worker.sh
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/web-worker-redis/README.md
mkdir public
curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/main/examples/web-worker-redis/public/index.html
git init -b main
git add .
git commit -m "Deploy web worker redis"
git remote add sshdock git@sshdock.example.com:web-worker-redis.git
git push sshdock main
```

Verify:

```bash
sudo sshdock apps list
sudo sshdock domains list web-worker-redis
sudo sshdock logs web-worker-redis worker
curl -I http://web-worker-redis.example.com
curl -fsS https://web-worker-redis.example.com
ssh -T sshdock@sshdock.example.com
```

Expected evidence:

- `apps list` shows `web-worker-redis healthy local`.
- `domains list web-worker-redis` includes `web-worker-redis.example.com`, service `web`, and port `18084`.
- HTTP redirects to HTTPS.
- HTTPS returns `SSHDock web worker Redis OK`.
- Worker logs show Redis `PONG`.
- Redis has no public route.

Clean up:

```bash
sudo sshdock apps remove web-worker-redis --force
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_web-worker-redis-' || true
```

No Docker volumes need to be removed.

## API Postgres

Path:

```text
examples/api-postgres
```

This example proves a routed API service with a private Postgres database and a named data volume. It uses demo credentials only.

Deploy:

```bash
mkdir api-postgres
cd api-postgres
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/api-postgres/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/api-postgres/README.md
mkdir db
curl -fsSLo db/init.sql https://raw.githubusercontent.com/sshdock/sshdock/main/examples/api-postgres/db/init.sql
git init -b main
git add .
git commit -m "Deploy API Postgres"
git remote add sshdock git@sshdock.example.com:api-postgres.git
git push sshdock main
```

Verify:

```bash
sudo sshdock apps list
sudo sshdock domains list api-postgres
sudo sshdock logs api-postgres api
sudo sshdock logs api-postgres db
curl -fsS https://api-postgres.example.com/messages
ssh -T sshdock@sshdock.example.com
```

Expected evidence:

- `apps list` shows `api-postgres healthy local`.
- `domains list api-postgres` includes `api-postgres.example.com`, service `api`, and port `18085`.
- HTTPS `/messages` returns JSON containing `SSHDock API Postgres OK`.
- The database service has no public route.
- The Postgres data lives in a named volume.

Clean up:

```bash
sudo sshdock apps remove api-postgres --force
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_api-postgres-' || true
sudo docker volume ls --format '{{.Name}}' | grep '^sshdock_api-postgres_'
```

The named volume remains because SSHDock preserves app data on removal. To delete the demo database too:

```bash
sudo docker volume rm sshdock_api-postgres_postgres-data
```

## Stateful Counter

Path:

```text
examples/stateful-counter
```

This example proves simple persistent state. Each HTTPS request increments a counter stored in a named Docker volume. It is the preferred future backup/restore demo candidate.

Deploy:

```bash
mkdir stateful-counter
cd stateful-counter
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/stateful-counter/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/stateful-counter/Dockerfile
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/stateful-counter/server.py
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/stateful-counter/README.md
git init -b main
git add .
git commit -m "Deploy stateful counter"
git remote add sshdock git@sshdock.example.com:stateful-counter.git
git push sshdock main
```

Verify:

```bash
curl -fsS https://stateful-counter.example.com
curl -fsS https://stateful-counter.example.com
sudo sshdock apps redeploy stateful-counter
curl -fsS https://stateful-counter.example.com
sudo sshdock domains list stateful-counter
sudo sshdock logs stateful-counter web
ssh -T sshdock@sshdock.example.com
```

Expected evidence:

- The counter increases across requests.
- The counter still increases after redeploy because the named volume is preserved.
- `domains list stateful-counter` includes `stateful-counter.example.com`, service `web`, and port `18086`.

Clean up:

```bash
sudo sshdock apps remove stateful-counter --force
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_stateful-counter-' || true
sudo docker volume ls --format '{{.Name}}' | grep '^sshdock_stateful-counter_'
```

To delete the demo counter data too:

```bash
sudo docker volume rm sshdock_stateful-counter_counter-data
```

## Rollback Lab

Path:

```text
examples/rollback-lab
```

This example proves a bad deploy and rollback flow.

Deploy the good release:

```bash
mkdir rollback-lab
cd rollback-lab
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/rollback-lab/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/rollback-lab/README.md
mkdir public
curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/main/examples/rollback-lab/public/index.html
git init -b main
git add .
git commit -m "Deploy rollback lab"
GOOD_COMMIT=$(git rev-parse HEAD)
git remote add sshdock git@sshdock.example.com:rollback-lab.git
git push sshdock main
curl -fsS https://rollback-lab.example.com
```

Create a bad deploy:

```bash
perl -0pi -e 's/image: nginx:alpine/image: nginx:no-such-tag-for-rollback-lab/' compose.yml
git add compose.yml
git commit -m "Break rollback lab image"
git push sshdock main
```

Rollback:

```bash
git push --force sshdock "$GOOD_COMMIT:main"
curl -fsS https://rollback-lab.example.com
sudo sshdock events list rollback-lab
```

Expected evidence:

- The Git push may complete because SSHDock deploys from a post-receive hook.
- The bad deploy fails and records `deploy.failed` with `stage`, `detail`, `changed`, `fix`, and `retry` fields.
- Remote `main` remains at the broken commit after its failed post-receive deployment.
- Force-pushing the saved good commit to `main` records a normal push deployment for that commit.
- HTTPS returns `SSHDock rollback lab OK` after rollback.

Clean up:

```bash
sudo sshdock apps remove rollback-lab --force
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_rollback-lab-' || true
```

No Docker volumes need to be removed.

## Custom Domain Or Manual Route

Use any routed example when the default `<app>.<base-domain>` route is not enough:

```bash
sudo sshdock domains attach static-site web app.example.com --port 18080
sudo sshdock domains list static-site
curl -fsS https://app.example.com
```

Expected evidence:

- `domains list static-site` includes the manually attached domain.
- Caddy serves HTTPS for the manual domain after DNS points at the server.
- The dashboard shows the added route.

## WordPress Lite

Path:

```text
examples/wordpress-lite
```

This example proves a small database-backed Compose app: WordPress, MariaDB, named volumes, one public web service, and automatic route inference. It uses the [WordPress Docker Official Image](https://hub.docker.com/_/wordpress) and [MariaDB Docker Official Image](https://hub.docker.com/_/mariadb).

MariaDB has a healthcheck, and WordPress waits for the database service to become healthy before starting. The first public request may still need a short retry window while WordPress copies its initial files and redirects to the installer.

Deploy:

```bash
mkdir wordpress-lite
cd wordpress-lite
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/wordpress-lite/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/wordpress-lite/README.md
git init -b main
git add .
git commit -m "Deploy WordPress lite"
git remote add sshdock git@sshdock.example.com:wordpress-lite.git
git push sshdock main
```

Verify on the server:

```bash
sudo sshdock apps list
sudo sshdock domains list wordpress-lite
sudo sshdock events list wordpress-lite
sudo sshdock logs wordpress-lite web
sudo sshdock logs wordpress-lite db
ssh -T sshdock@sshdock.example.com
```

Verify from your machine:

```bash
curl -I http://wordpress-lite.example.com
curl -fsSI https://wordpress-lite.example.com
```

Expected evidence:

- `apps list` shows `wordpress-lite healthy local`.
- `domains list wordpress-lite` includes `wordpress-lite.example.com`, service `web`, and port `18081`.
- `events list wordpress-lite` includes `deploy.succeeded`, `route.auto_attached`, and `router.reloaded`.
- HTTP returns a redirect to HTTPS.
- HTTPS reaches the WordPress first-load installer.
- `ssh -T sshdock@sshdock.example.com` shows the app, route, release, deployment, events, and logs.

The inline WordPress credentials are demo-only. Change them before real use. WordPress and MariaDB state live in Docker named volumes, and SSHDock v0 intentionally preserves volumes when removing an app. Operators remain responsible for secrets, backup/restore, WordPress updates, plugin updates, SMTP, cache, and hardening.

This example is a technical Compose and volume demo. It is not a promise of non-technical managed WordPress hosting.

Clean up:

On the SSHDock server:

```bash
sudo sshdock apps remove wordpress-lite --force
sudo sshdock apps list
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_wordpress-lite-' || true
sudo docker volume ls --format '{{.Name}}' | grep '^sshdock_wordpress-lite_'
```

On your machine, remove the scratch copy:

```bash
cd ..
rm -rf wordpress-lite
```

Expected cleanup evidence:

- `apps list` no longer shows `wordpress-lite`.
- No Docker containers named `sshdock_wordpress-lite-*` remain.
- The named volumes still exist because SSHDock v0 preserves app data on removal.
- The local `wordpress-lite` directory is removed from your machine.

To delete the demo WordPress data too, remove the named volumes explicitly after the app has been removed:

```bash
sudo docker volume rm sshdock_wordpress-lite_wordpress-data sshdock_wordpress-lite_mariadb-data
```

Only run the volume removal command when you intentionally want to erase the demo uploads, plugins, themes, and database.

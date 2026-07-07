# SSHDock Examples

These examples are small public confidence checks for the v0 happy path:

```text
git push -> first app creation -> Compose deploy -> default route -> HTTPS -> SSH dashboard visibility
```

The config example also proves the required-config guard before Compose starts.

They are meant to be copied into a new local directory and pushed to a real SSHDock server. The deploy commands below fetch the example files from GitHub without cloning this repository. Replace `example.com` with the base domain configured by `sudo sshdock server domain set <domain>`.

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
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/static-site/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/static-site/README.md
mkdir public
curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/main/examples/static-site/public/index.html
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
ssh -T dashboard@sshdock.example.com
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
- `ssh -T dashboard@sshdock.example.com` shows the app, route, release, deployment, events, and logs.

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

## Config App

Path:

```text
examples/config-app
```

This example proves app-scoped config without committing values: `.sshdock.yml` declares `APP_MESSAGE`, Compose references `${APP_MESSAGE}`, the first push fails before Compose starts, and the second push succeeds after the value is stored over SSH.

The app renders `APP_MESSAGE` publicly, so use a non-secret demo value for this example. The same config feature can store secrets, but applications should not return real secrets in HTTP responses.

Deploy:

```bash
mkdir config-app
cd config-app
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/config-app/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/config-app/.sshdock.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/config-app/Dockerfile
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/config-app/server.py
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/config-app/README.md
git init -b main
git add .
git commit -m "Deploy config app"
git remote add sshdock git@sshdock.example.com:config-app.git
git push sshdock main
```

Expected first-push evidence:

- The remote creates `config-app`.
- Deploy fails with `missing required config for config-app: APP_MESSAGE`.
- The error includes `ssh dashboard@sshdock.example.com config set config-app APP_MESSAGE`.
- Docker Compose does not start.

Set the missing value:

```bash
printf '%s\n' 'Hello from SSHDock config' | ssh dashboard@sshdock.example.com config set config-app APP_MESSAGE
ssh dashboard@sshdock.example.com config list config-app
ssh dashboard@sshdock.example.com config get config-app APP_MESSAGE
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
ssh -T dashboard@sshdock.example.com
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
- `ssh -T dashboard@sshdock.example.com` shows the app, route, release, deployment, events, logs, and redacted config.

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
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/wordpress-lite/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/wordpress-lite/README.md
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
ssh -T dashboard@sshdock.example.com
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
- `ssh -T dashboard@sshdock.example.com` shows the app, route, release, deployment, events, and logs.

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

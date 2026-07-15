# Migrating From Dokploy

This guide covers simple migrations from Dokploy to SSHDock. Keep Dokploy running until SSHDock has passed every verification step for the app.

SSHDock is not a Dokploy compatibility layer. It does not provide a web dashboard, provider polling, teams, RBAC, multi-server deployment, Docker Stack, built-in volume backups, scheduled jobs, notifications, or enterprise features. Migrate only apps that can run as Docker Compose on one SSHDock node.

## Good First App

Start with an app that has:

- A Docker Compose file you can commit to Git.
- One public web service and optional private services.
- Environment variables that can move to SSHDock config.
- Domains you can verify before cutover.
- Data that can be moved through a database dump, file copy, Docker volume copy, or Dokploy backup export.

## 1. Inventory The Dokploy App

Record:

- Project and service names.
- Deployment source: Git provider, Docker image, raw Docker Compose, or uploaded artifact.
- Compose file content and any custom command settings.
- Environment variables stored in the UI or `.env`.
- Domains and TLS behavior.
- Volumes, file mounts, and backup settings.
- Deployment history, current image or commit, and rollback point.
- Any schedules, webhooks, provider auto-deploy settings, notifications, remote servers, or cluster settings.

If the app depends on Dokploy multi-server, Stack/Swarm, provider automation, or the web UI for routine operation, do not treat this as a mechanical migration.

## 2. Move Compose Into A Git Repo

SSHDock deploys from Git. Create or reuse a repo with a root-level Compose file:

```text
compose.yml
```

For Dokploy raw Docker Compose services, copy the compose content into that file. For application deployments that used a Dockerfile or image, create a Compose service around the image or build.

Example:

```yaml
services:
  web:
    image: ghcr.io/example/my-app:latest
    ports:
      - "127.0.0.1:3000:3000"
```

Check [`COMPOSE_SUPPORT.md`](COMPOSE_SUPPORT.md) before pushing. Docker Compose validates the standard application model. Keep exactly one conventional Compose file at the repository root, without top-level `include` or service `extends.file` references to another Compose file.

## 3. Move Environment Values To SSHDock Config

Dokploy can store Docker Compose environment variables in a `.env` file next to the compose file. SSHDock stores values in encrypted host state and passes them to Compose at deploy time.

Declare required values where Compose uses them:

```yaml
services:
  web:
    image: ghcr.io/example/my-app:latest
    environment:
      DATABASE_URL: ${DATABASE_URL:?set DATABASE_URL with sshdock config set}
      API_KEY: ${API_KEY:?set API_KEY with sshdock config set}
    ports:
      - "127.0.0.1:3000:3000"
```

After the app exists on SSHDock, import or set values:

```bash
ssh dashboard@sshdock.example.com config import my-app < .env.production
ssh dashboard@sshdock.example.com config list my-app
```

Do not commit `.env.production`.

## 4. Prepare The SSHDock Server

```bash
sudo sshdock diagnostics
sudo sshdock server domain set example.com
cat ~/.ssh/id_ed25519.pub | sudo sshdock ssh-keys add admin
```

DNS:

```text
sshdock.example.com  A/AAAA  <server-ip>
*.example.com        A/AAAA  <server-ip>
```

## 5. Push To SSHDock

```bash
git remote add sshdock git@sshdock.example.com:my-app.git
git push sshdock main
```

Verify:

```bash
sudo sshdock apps health my-app
sudo sshdock domains list my-app
sudo sshdock domains check my-app
sudo sshdock releases list my-app
sudo sshdock events list my-app
sudo sshdock logs my-app --tail 200
ssh -T dashboard@sshdock.example.com
```

If automatic routing is not correct, attach a route explicitly:

```bash
sudo sshdock domains attach my-app web app.example.com --port 3000
sudo sshdock domains check my-app
```

## 6. Move Data

SSHDock does not import Dokploy volumes or file mounts automatically.

Use one deliberate path:

- Databases: dump from the Dokploy service, restore into the SSHDock Compose database service, then verify application reads and writes.
- Named volumes: restore from a Dokploy volume backup when available, or copy volume contents while writes are stopped.
- Bind mounts and file mounts: copy data into the host path or named volume used by the SSHDock Compose file.

Keep Dokploy and SSHDock from writing to the same data simultaneously.

SSHDock preserves Docker volumes on app removal. SSHDock backups include SSHDock host state and Docker volume inventory, not volume contents.

## 7. Cut Over

Before DNS cutover:

```bash
sudo sshdock diagnostics
sudo sshdock apps health my-app
sudo sshdock backup create
```

Also verify HTTPS externally, dashboard output, logs, rollback, and the app's critical user flow.

Then update DNS or attach the production domain to the SSHDock app:

```bash
sudo sshdock domains attach my-app web app.example.com --port 3000
sudo sshdock domains check my-app
```

## 8. Keep A Fallback

Keep the Dokploy project, backups, and DNS rollback path until SSHDock has handled normal traffic, deploys, rollback, and restart/recovery for the app.

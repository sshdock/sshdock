# Migrating From Dokku

This guide covers simple migrations from Dokku to SSHDock. It is deliberately conservative. Keep Dokku running until SSHDock has passed every verification step for the app.

SSHDock is not a full Dokku compatibility layer. It does not run Procfiles, buildpacks, Dokku plugins, Dokku service plugins, or Dokku scheduler features. Migrate only apps that can be represented as Docker Compose on one node.

## Good First App

Start with an app that has:

- One public web service.
- A Dockerfile or image that can be referenced from Compose.
- A small set of environment variables.
- State that can be moved with a database dump, file copy, or Docker volume copy.
- A rollback path you can test before DNS cutover.

Do not start with your most stateful or plugin-heavy app.

## 1. Inventory The Dokku App

Record the current production shape before changing anything:

- App name and Git remote.
- Build type: buildpack, Dockerfile, image, or custom builder.
- Public domains and TLS expectations.
- Config keys and secret values.
- Persistent storage mounts or database/service plugins.
- Current release or image version.
- Any deploy hooks, release tasks, cron jobs, one-off commands, or plugin behavior.

Common Dokku starting points include `dokku apps:list`, domain commands, config commands, storage commands, and backup guidance. Verify the exact commands against the Dokku docs for your installed version.

## 2. Convert The App To Compose

SSHDock expects one root-level Compose file:

- `compose.yaml`
- `compose.yml`
- `docker-compose.yaml`
- `docker-compose.yml`

If the app uses a Dockerfile, create a service with `build: .`:

```yaml
services:
  web:
    build: .
    ports:
      - "127.0.0.1:3000:3000"
```

If the app already uses an image, reference it directly:

```yaml
services:
  web:
    image: ghcr.io/example/my-app:latest
    ports:
      - "127.0.0.1:3000:3000"
```

For automatic routing, prefer one public web service with one loopback-published TCP port. Private workers, Redis, Postgres, and other dependencies can be additional Compose services without public ports.

Check [`COMPOSE_SUPPORT.md`](COMPOSE_SUPPORT.md) before pushing. Docker Compose validates standard application fields, including networks, secrets, configs, resources, labels, commands, and entrypoints. Keep exactly one conventional Compose file at the repository root, without top-level `include` or service `extends.file` references to another Compose file.

## 3. Move Config Without Committing Secrets

Do not commit `.env` files with secret values.

Declare required values where Compose uses them:

```yaml
services:
  web:
    build: .
    environment:
      DATABASE_URL: ${DATABASE_URL:?set DATABASE_URL with sshdock config set}
      SECRET_KEY_BASE: ${SECRET_KEY_BASE:?set SECRET_KEY_BASE with sshdock config set}
    ports:
      - "127.0.0.1:3000:3000"
```

After the SSHDock app exists, store values over operator SSH:

```bash
ssh sshdock@sshdock.example.com config set my-app DATABASE_URL < database-url.txt
ssh sshdock@sshdock.example.com config set my-app SECRET_KEY_BASE < secret-key-base.txt
ssh sshdock@sshdock.example.com config list my-app
```

If you push before setting required config, Docker Compose validation fails before containers start and names the missing variable.

## 4. Prepare The SSHDock Server

Install SSHDock and set the base domain:

```bash
sudo sshdock diagnostics
sudo sshdock server domain set example.com
cat ~/.ssh/id_ed25519.pub | sudo sshdock ssh-keys add admin
```

Point DNS at the SSHDock server:

```text
sshdock.example.com  A/AAAA  <server-ip>
*.example.com        A/AAAA  <server-ip>
```

## 5. Push To SSHDock

From the migrated app repo:

```bash
git remote add sshdock git@sshdock.example.com:my-app.git
git push sshdock main
```

Verify on the server:

```bash
sudo sshdock apps health my-app
sudo sshdock domains list my-app
sudo sshdock domains check my-app
sudo sshdock releases list my-app
sudo sshdock events list my-app
sudo sshdock logs my-app --tail 200
ssh -T sshdock@sshdock.example.com
```

If automatic route inference is not enough, attach the route explicitly:

```bash
sudo sshdock domains attach my-app web app.example.com --port 3000
sudo sshdock domains check my-app
```

## 6. Move Persistent Data

SSHDock does not import Dokku storage automatically.

Use an app-specific plan:

- Databases: take a dump from the Dokku app or service, restore it into the SSHDock Compose database service, then verify application reads and writes.
- Uploaded files: copy from Dokku storage mounts to the target host path or Docker volume used by the SSHDock Compose app.
- Docker named volumes: stop writes, copy data with a one-off Docker container or a tested backup/restore tool, then start the SSHDock app.

Keep both systems from writing to the same data at the same time.

SSHDock app removal preserves Docker volumes. SSHDock backups include volume inventory by default, not volume contents.

## 7. Test Rollback And Recovery

Before DNS cutover:

```bash
sudo sshdock releases list my-app
sudo sshdock apps redeploy my-app
sudo sshdock apps rollback my-app <release-id>
sudo sshdock apps health my-app
sudo sshdock backup create
```

For a bad-deploy rehearsal, use [`examples/rollback-lab`](../examples/rollback-lab/README.md) as the reference pattern.

## 8. Cut Over DNS

Only cut over after:

- `sudo sshdock diagnostics` passes.
- `sudo sshdock apps health my-app` is `ok`.
- `sudo sshdock domains check my-app` is `ok` or the route state is otherwise understood.
- HTTPS works externally.
- Logs and events show the expected release.
- Rollback has been tested.
- Backups are created and restorable enough for your risk tolerance.

Then move the production app domain to the SSHDock server or attach the production domain to the SSHDock app and update DNS.

## 9. Keep A Fallback

Keep the Dokku app, data, and DNS rollback plan until the SSHDock app has run long enough to cover normal traffic and background jobs. Do not delete Dokku storage or plugin-managed data as part of the first cutover.

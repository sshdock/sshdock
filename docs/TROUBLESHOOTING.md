# SSHDock Troubleshooting

Start with the smallest command that answers the current question. SSHDock records most deploy and route state in SQLite, so CLI and SSH dashboard output should agree.

## Install Or Upgrade Looks Wrong

Run:

```bash
sudo sshdock diagnostics
```

Diagnostics checks config, runtime directories, Docker, Compose, Caddy, OpenSSH, Git, `sshdockd.service`, ports, DNS, generated Caddy import wiring, forced-command `authorized_keys`, config-key permissions, and SQLite migrations.

Read the `why` and `fix` lines for the failed check. After an upgrade, also run:

```bash
sudo sshdock apps health <app>
sudo sshdock domains check <app>
sudo sshdock logs <app> --tail 200
```

## Git Push Fails Before Deploy

Check:

- The remote is `git@sshdock.<domain>:<app>.git`.
- `sudo sshdock diagnostics` passes.
- The deploy key was added with `sudo sshdock ssh-keys add <name>`.
- The app repo contains exactly one root `compose.yaml`, `compose.yml`, `docker-compose.yaml`, or `docker-compose.yml`.

If the push reaches SSHDock but deploy fails, use the deploy-failure section below.

## Deploy Fails

Failed deploys print and persist:

```text
stage=...
detail=...
changed=...
fix=...
retry=...
```

Inspect the same failure from multiple surfaces:

```bash
sudo sshdock apps health <app>
sudo sshdock releases list <app>
sudo sshdock events list <app>
ssh -T dashboard@sshdock.<domain>
```

Common cases:

- `missing required config`: set the named key with `ssh dashboard@sshdock.<domain> config set <app> <key>`, then push or redeploy.
- Compose validation failure: run `docker compose config` in the app repository, then fix the reported Compose error.
- Image pull or build failure: fix the image reference, registry access, Dockerfile, or build context, then push again.
- Service start or health-wait failure: inspect `sudo sshdock apps health <app>` and `sudo sshdock logs <app> --tail 200`, then fix services that exited, became unhealthy, or exceeded the bounded wait before redeploying.
- Caddy reload failure: run `sudo sshdock domains check <app>` and `sudo sshdock diagnostics`, then fix route or Caddy config state.

Stored config values are redacted from deploy output, events, logs, and dashboard views.

## Required Config Is Missing Or Stale

List configured keys without revealing values:

```bash
ssh dashboard@sshdock.<domain> config list <app>
```

Set or import values:

```bash
ssh dashboard@sshdock.<domain> config set <app> DATABASE_URL < database-url.txt
ssh dashboard@sshdock.<domain> config import <app> < .env.production
```

Redeploy so containers receive changed values:

```bash
sudo sshdock apps redeploy <app>
```

Use `config get` only when you intentionally need to reveal a value.

## Domain Or HTTPS Route Is Wrong

Check DNS first:

```text
sshdock.<domain>  A/AAAA  <server-ip>
*.<domain>        A/AAAA  <server-ip>
```

Then inspect SSHDock state:

```bash
sudo sshdock domains list <app>
sudo sshdock domains check <app>
sudo sshdock events list <app>
sudo sshdock diagnostics
```

If automatic route inference picked no route or the wrong route, attach one explicitly:

```bash
sudo sshdock domains attach <app> <service> app.example.com --port 3000
sudo sshdock domains check <app>
```

`domains check` reports stored domain rows and router comparison status when the configured router can report routes.

## Logs Are Too Short Or Need A Service

Use:

```bash
sudo sshdock logs <app> --tail 200
sudo sshdock logs <app> <service> --tail 200
sudo sshdock logs <app> <service> --tail 200 -f
```

The default tail is 100 lines.

## SSH Dashboard Does Not Open

Use the documented dashboard commands:

```bash
ssh dashboard@sshdock.<domain>
ssh -T dashboard@sshdock.<domain>
```

Do not append a remote `dashboard` command. The installed OpenSSH forced command already runs `sshdockd dashboard`; remote commands are reserved for config commands such as:

```bash
ssh dashboard@sshdock.<domain> config list <app>
```

If access fails:

```bash
sudo sshdock diagnostics
sudo sshdock ssh-keys list
```

Re-add the key if needed:

```bash
cat ~/.ssh/id_ed25519.pub | sudo sshdock ssh-keys add admin
```

## App Looks Unhealthy After A Bad Deploy

Inspect:

```bash
sudo sshdock apps health <app>
sudo sshdock releases list <app>
sudo sshdock events list <app>
sudo sshdock logs <app> --tail 200
```

If a previous release is known-good:

```bash
sudo sshdock apps rollback <app> <release-id>
sudo sshdock apps health <app>
```

`apps health` reports the newest failed release/deployment and failure detail while checking Compose service status against the latest runnable release when one exists.

## Removing An App Did Not Remove Data

This is expected. SSHDock removes app metadata, app repo/worktree, and containers, but it does not pass Compose `--volumes` or prune Docker images and build cache.

Back up data before removing volumes manually:

```bash
sudo sshdock backup create
sudo docker volume ls
```

Then remove only volumes you have identified as app-specific and no longer needed.

## Backup Or Restore Questions

Create and inspect backups:

```bash
sudo sshdock backup create
sudo sshdock backup inspect /var/lib/sshdock/backups/<archive>.tar.gz
```

Restore on a compatible single-node SSHDock install:

```bash
sudo systemctl stop sshdockd
sudo sshdock backup restore /path/to/archive.tar.gz
sudo sshdock diagnostics
sudo systemctl start sshdockd
```

Backups include SSHDock host state, app repos/worktrees, SQLite metadata, `config.key`, Git/dashboard key state, generated Caddy config, and Docker volume inventory. They do not silently include Docker volume contents.

## Still Unsure

Collect the minimum useful evidence:

```bash
sudo sshdock diagnostics
sudo sshdock apps health <app>
sudo sshdock domains check <app>
sudo sshdock releases list <app>
sudo sshdock events list <app>
sudo sshdock logs <app> --tail 200
```

Keep private hostnames, IPs, key paths, fingerprints, and raw logs out of public issues or docs.

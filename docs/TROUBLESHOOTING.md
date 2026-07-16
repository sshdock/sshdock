# SSHDock Troubleshooting

Start with the smallest command that answers the current question. SSHDock records most deploy and route state in SQLite, so CLI and SSH dashboard output should agree.

## Install Or Upgrade Looks Wrong

Run:

```bash
sudo sshdock diagnostics
```

Diagnostics checks config, runtime directories, Docker, Compose, Caddy, OpenSSH, Git, `sshdockd.service`, ports, DNS, generated Caddy import wiring, forced-command `authorized_keys`, config-key permissions, SQLite migrations, and reboot-risk warnings for routed or running services.

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

SSHDock deploys in `post-receive`, so a deployment failure does not undo an accepted Git update. The output labels `git: remote main updated ...` separately from `deploy: ... succeeded` or `deploy: failed ...`.

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
ssh -T sshdock@sshdock.<domain>
```

Common cases:

- Missing `${KEY:?message}` Compose config: set the named key with `ssh sshdock@sshdock.<domain> config set <app> <key>`, then push or redeploy.
- Compose validation failure: run `docker compose config` in the app repository, then fix the reported Compose error.
- Image pull or build failure: fix the image reference, registry access, Dockerfile, or build context, then push again.
- Service start or health-wait failure: inspect `sudo sshdock apps health <app>` and `sudo sshdock logs <app> --tail 200`, then fix services that exited, became unhealthy, or exceeded the bounded wait before redeploying.
- Caddy reload failure: run `sudo sshdock domains check <app>` and `sudo sshdock diagnostics`, then fix route or Caddy config state.

Stored config values are redacted from deploy output, events, logs, and dashboard views.

## Required Config Is Missing Or Stale

List configured keys without revealing values:

```bash
ssh sshdock@sshdock.<domain> config list <app>
```

Set or import values:

```bash
ssh sshdock@sshdock.<domain> config set <app> DATABASE_URL < database-url.txt
ssh sshdock@sshdock.<domain> config import <app> < .env.production
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
ssh sshdock@sshdock.<domain>
ssh -T sshdock@sshdock.<domain>
```

Do not append a remote `operator` command. The installed OpenSSH forced command already runs `sshdockd operator`; append only a supported inspection or config command, such as:

```bash
ssh sshdock@sshdock.<domain> config list <app>
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

If a previous commit or tag is known-good, select it as remote `main`:

```bash
git push --force sshdock <known-good-commit-or-lightweight-tag>:main
git push --force sshdock '<known-good-annotated-tag>^{}:refs/heads/main'
sudo sshdock apps health <app>
```

This Git push records a normal deployment attempt for the selected commit. Use `apps redeploy` only when you want to retry the commit already at remote `main`, such as after fixing server-side config or a transient runtime failure.

`apps health` reports the newest failed release/deployment and failure detail while checking the Compose file and service state from the app's current worktree.

## Restart Policy Warning After Reboot

SSHDock does not redeploy apps during daemon startup. If `diagnostics` or `apps health` reports a restart-policy warning, add an appropriate Compose policy to each named routed or long-running service, commit it, and push:

```yaml
services:
  web:
    restart: unless-stopped
```

Use `restart: always` only when that stronger behavior matches the service. Leave intentionally finite jobs without a restart policy. After changing the Compose file, push the commit to remote `main`; a plain `apps restart` does not apply Compose-file changes.

## App Start Says Containers Are Missing

`apps start` maps literally to Compose start and never changes into a deployment. If the app's containers were removed, apply the commit currently at remote `main` again:

```bash
sudo sshdock apps redeploy <app>
```

## App Exec Says A Service Is Missing Or Stopped

`apps exec` only targets an existing running Compose service container. Check current service state first:

```bash
sudo sshdock apps health <app>
sudo sshdock apps start <app>
```

If the service is absent because its containers were removed or the Compose model changed, redeploy current remote `main`. Use `apps run <app> <service> -- <command>` when the operation should run in a new removable one-off container instead of an existing service.

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

Backups include SSHDock host state, app repos/worktrees, SQLite metadata, `config.key`, Git/operator key state, generated Caddy config, and Docker volume inventory. They do not silently include Docker volume contents.

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

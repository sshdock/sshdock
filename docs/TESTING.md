# Testing

This guide explains SSHDock's local and end-to-end test tiers.

The main product spine under test is:

```text
git remote add sshdock git@server:<app>.git -> git push -> sshdockd git-receive -> app + bare repo -> post-receive hook -> sshdockd git-hook -> SQLite release/deployment records
```

## Test Tiers

The default e2e test uses real local Git commands, a fake SSH transport script, and a real bare repository.

The default e2e still uses fake runtime adapters for:

- Docker Compose deployment
- Caddy routing
- SSH dashboard sessions

This keeps the default real pass focused on Git receive and release recording. Real OpenSSH is covered by `make ssh-e2e`; real Docker is covered by the opt-in Docker tier; Caddy routing is covered by `make route-e2e`.
The SSH dashboard is covered by `make tui-e2e`. Recovery state transitions and deploy-failure persistence are covered by `make recovery-e2e`. Production hardening checks are covered by `make hardening-e2e`. Backup archive restore is covered by `make backup-restore-e2e`.

## Internal Dogfood Readiness

Before accepting a dogfood release line, run the normal local gate plus the focused install, config, and hardening harnesses:

```bash
make ci
make bootstrap-e2e
make config-e2e
make hardening-e2e
make backup-restore-e2e
```

The local harnesses do not replace VPS dogfood. The release acceptance pass should still install or upgrade from public assets with no local overrides, push representative examples through public Git SSH, verify dashboard and CLI lifecycle commands, exercise config redaction and rollback, verify reboot recovery and final-route cleanup, and complete the documented backup/restore drill. At minimum, a dogfood release should cover static-site, build-service, config-backed, one multi-service dependency example, one stateful volume example, and rollback-lab. Keep raw VPS output and host-specific details in private local artifacts; public docs and trackers should record only summarized acceptance.

## Command

Run:

```bash
make e2e
```

The current e2e target:

1. Build `sshdock` and `sshdockd`.
2. Create a temporary SSHDock data directory.
3. Verify explicit setup still works:

```bash
sshdock apps create my-app
```

4. Verify SQLite contains the app record.
5. Verify the app repo exists at:

```text
<data-dir>/apps/my-app/repo.git
```

6. Verify the repo has an executable `hooks/post-receive` file.
7. Create a local source repository with a supported `compose.yml`.
8. Push `main` to the created bare repository path.
9. Let the hook invoke:

```bash
sshdockd git-hook --app my-app --repo <repo.git>
```

10. Verify SQLite contains:
   - one release for the pushed commit
   - one succeeded deployment for that release
11. Verify push-to-create through a fake SSH transport:

```bash
git remote add sshdock git@server:push-app.git
git push sshdock main
```

12. Verify `sshdockd git-receive` creates the app and records the deployment.

## Caddy Route Tier

Run:

```bash
make route-e2e
```

This tier requires Docker, OpenSSH, Caddy, and curl. If Caddy is not installed, the test skips.

The route test:

1. Builds and installs SSHDock binaries under a fake root.
2. Starts a temporary local OpenSSH server.
3. Starts a temporary local Caddy process with a private admin address.
4. Pushes a Compose app that publishes nginx on a random `127.0.0.1:<port>`.
5. Runs `sshdock domains attach <app> web http://127.0.0.1:<caddy-port> --port <published-port>`.
6. Verifies Caddy serves the app through the generated route.
7. Verifies SQLite contains the domain and route events.

The local test uses explicit `http://127.0.0.1:<port>` addresses to avoid public DNS, ACME, and privileged ports. Production public domains rely on DNS pointing at the server and Caddy's normal HTTP/HTTPS handling.

## Wildcard Base Domain Tier

Run:

```bash
make wildcard-domain-e2e
```

This tier uses fake Compose and fake Caddy runners so it does not need public DNS. It configures `sshdock server domain set example.com`, pushes a Compose app with `web` publishing `127.0.0.1:<port>:80`, and verifies:

1. `apps create` prints `git@sshdock.example.com:<app>.git`.
2. The successful push persists `<app>.example.com` in SQLite.
3. The generated Caddyfile contains one explicit route for the app hostname, not a wildcard catch-all.
4. The SSH dashboard snapshot shows the auto-created route.
5. An ambiguous Compose app still deploys and records `route.auto_skipped` with manual attach guidance.

## SSH Dashboard Tier

Run:

```bash
make tui-e2e
```

The dashboard test:

1. Builds and installs SSHDock binaries under a fake root.
2. Starts a temporary local OpenSSH server for the Git push path.
3. Pushes a Compose app through real `ssh`.
4. Renders dashboard `authorized_keys` through `sshdock ssh-keys add`.
5. Starts a temporary local OpenSSH server using the dashboard `authorized_keys` file.
6. Connects with `ssh -T` and verifies the plain render-once fallback.
7. Connects with `ssh -tt`, lets the forced command run interactive `sshdockd dashboard`, sends `q`, and verifies PTY allocation succeeds.
8. Verifies the dashboard includes the app list, app detail, service status, route, release, deployment, event, and service log views.

This test uses the fake Compose runner for dashboard service status/log output so it does not require the deployed app to be running in Docker.

TUI app action coverage is separate:

```bash
make tui-actions-e2e
```

That target drives the interactive dashboard model against SQLite state, the shared CLI lifecycle backend, a fake Compose runner, and a fake router. It covers restart app, restart service, redeploy, rollback, domain attach, domain detach, app removal, and persisted event visibility. App removal is verified through the shared backend path and the fake Compose remove request, preserving the same volume-preserving contract as `sshdock apps remove`.

Focused CLI tests cover `sshdock apps health <app>`, `sshdock domains check <app>`, and `sshdock logs --tail <lines>`:

```bash
go test ./internal/cli -run 'Test(AppsHealth|DomainsCheck|LogsTail|StoreBackendAppsHealth|StoreBackendDomainsCheck)'
```

Focused adoption and example docs checks cover the comparison, migration, troubleshooting, and runnable example contracts:

```bash
go test ./test/harness -run 'Test(AdoptionDocs|Examples|ConfigExample|RollbackLab|WordPressExample|ProjectBranding)'
```

### Real SSH Dashboard Screenshot Capture

Run:

```bash
make tui-screenshots-real
```

This target uses the same real dashboard path as `make tui-e2e`:

1. Builds and installs SSHDock binaries under a fake root.
2. Starts a temporary local OpenSSH server for the Git push path.
3. Pushes a Compose app through real `ssh`.
4. Starts a temporary local OpenSSH server using the dashboard `authorized_keys` file.
5. Runs `ssh -tt` inside a fixed-size local PTY so OpenSSH allocates a real remote PTY for `sshdockd dashboard`.
6. Replays the PTY output through a headless terminal model and captures the live alternate-screen dashboard before quitting.
7. Writes artifacts to `_artifacts/tui-screenshots-real/`:
   - `session.ansi`: raw PTY output stream
   - `summary|services|routes|releases|deploys|logs.txt`: plain screen text for each captured tab
   - `summary|services|routes|releases|deploys|logs.png`: PNG screenshots for each captured tab
   - `manifest.json`: command, terminal size, and artifact index

The capture uses the fake Compose runner for service status/log data, but the dashboard access path is real OpenSSH forced-command dashboard access.

To capture a real external server instead of the local e2e harness, run:

```bash
SSHDOCK_TUI_SCREENSHOT_SSH_TARGET=dashboard@server \
SSHDOCK_TUI_SCREENSHOT_SSH_IDENTITY=/path/to/key \
make tui-screenshots-vps
```

This target does not bootstrap or mutate the server. It only opens `ssh -tt` to the configured dashboard target and writes artifacts to `_artifacts/tui-screenshots-vps/` by default. Optional variables:

- `SSHDOCK_TUI_SCREENSHOT_SSH_PORT`: SSH port
- `SSHDOCK_TUI_SCREENSHOT_DIR`: output directory
- `SSHDOCK_TUI_SCREENSHOT_ROWS`: terminal rows, default `32`
- `SSHDOCK_TUI_SCREENSHOT_COLS`: terminal columns, default `120`
- `SSHDOCK_TUI_SCREENSHOT_TIMEOUT`: per-frame wait timeout, default `20s`
- `SSHDOCK_TUI_SCREENSHOT_TABS`: maximum tab presses before stopping, default `8`

## Recovery Tier

Run:

```bash
make recovery-e2e
```

The recovery test:

1. Builds `sshdock` and `sshdockd`.
2. Creates an app and pushes a good Compose release through a local bare repository hook.
3. Pushes a second release with `SSHDOCK_FAKE_COMPOSE_DEPLOY_ERROR` to force a failed deploy.
4. Runs `sshdock apps rollback <app> <release-id>`.
5. Verifies app, release, deployment, event, and failure-detail state reflect the failed deploy followed by a successful rollback.

This test uses the fake Compose runner so it does not mutate Docker, Caddy, or host SSH state.

Focused unit tests for deploy failure classification, redaction, route inference, Caddy reload failure, release-list failure detail, and dashboard failure rendering live in:

```bash
go test ./internal/compose ./internal/gitrecv ./internal/app ./internal/cli ./internal/tui
```

## Hardening Tier

Run:

```bash
make hardening-e2e
```

The hardening test:

1. Builds `sshdock` and `sshdockd`.
2. Runs bootstrap under a temporary fake root.
3. Re-runs bootstrap to verify the install is idempotent.
4. Verifies existing `/var/lib/sshdock` state survives the second run.
5. Verifies stale installed binaries are replaced.
6. Sets a fake base domain and runs `sshdock diagnostics` with fake Docker, Caddy, SSH, Git, DNS, port, and systemd commands.

This test does not touch host systemd, Docker, Caddy, SSH, or `/var/lib/sshdock`.

## Backup Restore Tier

Run:

```bash
make backup-restore-e2e
```

The backup restore test:

1. Creates a real SQLite store with an app and encrypted config value.
2. Writes app repo/worktree state, Git/dashboard key state, and generated Caddy config under temporary paths.
3. Creates a backup archive with fake Docker volume inventory.
4. Restores the archive to a separate temporary config.
5. Verifies the restored `config.key` can decrypt the restored config value.
6. Verifies generated Caddy config and Docker volume inventory survived the archive round trip.

This test does not call real Docker, Caddy, SSH, systemd, or host `/var/lib/sshdock`.

## `sshdockd git-hook`

The hook command reads post-receive input from stdin:

```text
<old-sha> <new-sha> refs/heads/main
```

For this first real pass, the command should:

1. Load config from environment variables.
2. Open the SQLite store.
3. Check out the pushed commit into the configured app worktree.
4. Detect `compose.yml` or `docker-compose.yml`.
5. Validate the supported Compose subset.
6. Create a release record.
7. Create a deployment record.
8. Run the fake Compose deploy adapter.
9. Mark the deployment succeeded or failed.

## Environment

The e2e test uses:

```text
SSHDOCK_DATA_DIR=<temporary-data-dir>
SSHDOCK_COMPOSE_RUNNER=fake
```

`SSHDOCK_COMPOSE_RUNNER=fake` is intentional. It makes this test a real Git/hook/store pass without requiring Docker.

## Config Harness

Run:

```bash
make config-e2e
```

This target imports an app config value into encrypted SQLite storage, commits only `.sshdock.yml` plus Compose interpolation references, runs the post-receive deploy path with the fake Compose runner, and verifies the decrypted value reaches Docker Compose through the process environment instead of a committed `.env` file.

## Opt-In Docker E2E

Run:

```bash
make e2e-docker
```

The Docker e2e target:

1. Require Docker Engine and Docker Compose.
2. Build `sshdock` and `sshdockd`.
3. Create a temporary SSHDock data directory.
4. Run `sshdock apps create my-app`.
5. Push a Compose app to the created bare repository.
6. Run the hook with:

```text
SSHDOCK_COMPOSE_RUNNER=docker
```

7. Verify SQLite contains a succeeded deployment.
8. Verify Docker reports the app service as running.
9. Tear down the Docker Compose project after the test.

This test is opt-in because it uses the local Docker daemon and may pull images.

## Server Push E2E

Run:

```bash
make server-push-e2e
```

The server push e2e target:

1. Builds `sshdock` and `sshdockd`.
2. Runs `scripts/bootstrap.sh` under a temporary fake root.
3. Uses the installed binaries from that fake root.
4. Renders Git receive `authorized_keys` through `sshdock ssh-keys add`.
5. Starts a local unprivileged OpenSSH daemon.
6. Pushes an image-service Compose app through real `ssh` with the fake Compose runner.
7. Pushes a build-service Compose app through real `ssh` with the Docker Compose runner.
8. Verifies app, release, deployment, and event state in SQLite.
9. Verifies the build-service deploy writes the SSHDock release override and starts a running Docker Compose service.

The build-service test uses the local Docker daemon and may pull `nginx:alpine`.

## OpenSSH E2E

Run:

```bash
make ssh-e2e
```

The OpenSSH e2e target:

1. Builds `sshdock` and `sshdockd`.
2. Generates temporary client and host SSH keys.
3. Runs `sshdock ssh-keys add admin` to render an `authorized_keys` file.
4. Starts a local unprivileged `sshd` when the host supports it.
5. Pushes a Compose app through real `ssh`.
6. Lets the forced command run `sshdockd git-receive`.
7. Verifies SQLite contains a succeeded deployment.

The test skips only when the local OpenSSH server is unavailable or cannot be started with the test config.

## Non-Goals

The default and OpenSSH e2e tiers do not prove:

- dashboard SSH sessions
- production system SSH daemon reload behavior

The Caddy route path is covered separately by `make route-e2e`. The dashboard SSH path is covered separately by `make tui-e2e`.

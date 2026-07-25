# Health, logs, and history feature lab

## Purpose

This lab reuses the recovered Next.js application from [`examples/labs/failed-deploy-and-git-recovery`](../failed-deploy-and-git-recovery/README.md). It adds no application source, Dockerfile, Compose file, or patch. It turns that known failure and Git-selected recovery into one operator inspection pass across desired `main`, current Compose state, desired and active routes, bounded and followed logs, immutable releases, deployment attempts, and events.

Replace `example.com` below with the SSHDock base domain. The commands use the restricted SSHDock operator account; no host shell or local Docker is required.

## Prepare the recovered application

Follow the recovery lab through its **Recover through remote main** section, but do not run its cleanup. Keep its `GOOD_COMMIT` and `BAD_COMMIT` shell variables. The recovered app must be reachable at `https://failed-deploy-and-git-recovery.example.com` before continuing.

Run this lab from the SSHDock repository checkout:

```bash
SSHDOCK_TARGET=sshdock@sshdock.example.com \
SSHDOCK_APP=failed-deploy-and-git-recovery \
SSHDOCK_ROUTE_HOST=failed-deploy-and-git-recovery.example.com \
SSHDOCK_EXPECTED_MAIN=$GOOD_COMMIT \
SSHDOCK_FAILED_MAIN=$BAD_COMMIT \
bash examples/labs/health-logs-and-history/acceptance.sh
```

Set `SSHDOCK_IDENTITY_FILE=/path/to/key` too when SSH does not already select the operator key.

## What the acceptance script proves

The script captures `apps health "$APP"` before and after an explicit same-commit redeploy. It requires the desired remote Git ref to remain `SSHDOCK_EXPECTED_MAIN` while the latest successful deployment attempt is reported separately. It also requires a running service, an `ok` restart-policy check, the desired automatic domain, and an active Caddy route.

It then runs the supported inspection commands directly through the operator surface:

```bash
ssh sshdock@sshdock.example.com apps health "$APP"
ssh sshdock@sshdock.example.com domains list "$APP"
ssh sshdock@sshdock.example.com domains check "$APP"
ssh sshdock@sshdock.example.com logs "$APP" web --tail 20
ssh sshdock@sshdock.example.com logs "$APP" web --tail 20 -f
ssh sshdock@sshdock.example.com releases list "$APP"
ssh sshdock@sshdock.example.com deployments list "$APP"
ssh sshdock@sshdock.example.com events list "$APP"
ssh sshdock@sshdock.example.com apps redeploy "$APP"
```

Release history must remain the two immutable Git commits. Deployment history must gain exactly one successful `redeploy` attempt for `GOOD_COMMIT`; it does not create a rollback target. The deployment and event output retain the failed `BAD_COMMIT` evidence after recovery. The bounded follow check uses only portable shell process control and stops after a short observation window, so it does not leave a persistent SSH process.

## Cleanup

The acceptance script removes the app after its final health and history checks:

```bash
ssh sshdock@sshdock.example.com apps remove "$APP" --force
```

The Next.js probe is stateless and declares no named volumes. This lab does not alter DNS, Caddy configuration, or Docker volumes outside normal SSHDock app removal.

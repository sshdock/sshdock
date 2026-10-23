# Failed deploy and Git recovery feature lab

## Purpose

This lab reuses the Next.js framework compatibility probe to show the difference between accepting a Git ref and completing a healthy deployment. Its only overlay changes the copied probe's Compose build to name an absent Dockerfile. That creates a controlled build failure without copying or changing the canonical probe, its Dockerfile, or the generated application source.

This failure happens before Compose replaces containers. It does not prove recovery after an unhealthy replacement or a failed recovery attempt; see the [manual recovery guide and Docker test](../../../docs/RECOVERY.md). The structured recovery workflow requires the v1.1.0 CLI.

Replace `example.com` below with the SSHDock base domain.

## Deploy a known-good commit

Copy the Next.js deployment envelope and this one-file failure overlay from the matching `v1.1.0` release:

```bash
mkdir failed-deploy-and-git-recovery
cd failed-deploy-and-git-recovery
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/tags/v1.1.0.tar.gz \
  | tar -xz --strip-components=4 sshdock-1.1.0/examples/frameworks/nextjs
curl -fsSL https://raw.githubusercontent.com/sshdock/sshdock/v1.1.0/examples/labs/failed-deploy-and-git-recovery/failure.patch \
  -o failure.patch
git init -b main
git add .
git commit -m "Deploy Next.js recovery lab"
GOOD_COMMIT=$(git rev-parse HEAD)
git remote add sshdock git@sshdock.example.com:failed-deploy-and-git-recovery.git
git push sshdock main
curl -fsS https://failed-deploy-and-git-recovery.example.com
```

The first push proves the unmodified Next.js probe is healthy. Keep `GOOD_COMMIT`; recovery will push that known-good Git object back to remote `main`.

## Push the controlled failure

Apply the overlay, commit it, and push the new commit:

```bash
git apply failure.patch
git add compose.yml
git commit -m "Exercise controlled Next.js build failure"
BAD_COMMIT=$(git rev-parse HEAD)
git push sshdock main
```

The push can report a deployment failure after the ref update because SSHDock accepts Git before running the post-receive deployment. Do not interpret a failing deployment as a rejected Git ref.

## Inspect accepted Git and failed deployment

Compare `current main` with `BAD_COMMIT`, then inspect the supported SSHDock surfaces:

```bash
ssh sshdock@sshdock.example.com apps health failed-deploy-and-git-recovery
sudo sshdock releases list failed-deploy-and-git-recovery
sudo sshdock deployments list failed-deploy-and-git-recovery
sudo sshdock events list failed-deploy-and-git-recovery
```

`current main` reports `BAD_COMMIT` even though the deployment failed. Health and deployment history show the failed build attempt with actionable detail; release history retains both commits; events include `git.ref_accepted`, `deploy.started`, and `deploy.failed`. The Git receive account intentionally supports pushes only, so use the SSHDock operator command instead of `git ls-remote`. A failed deploy is not an SSHDock rollback operation.

## Recover through remote main

Review the saved good commit and require typing its full SHA before changing remote `main`. The expected SHA is the bad commit accepted above; another update causes a stale-lease rejection. Stop and inspect on rejection rather than refreshing the lease automatically:

```bash
EXPECTED_MAIN=$BAD_COMMIT
TARGET_COMMIT=$(git rev-parse --verify "$GOOD_COMMIT^{commit}")
git show --no-patch "$TARGET_COMMIT"
printf 'Expected remote main: %s\nRecovery target: %s\n' "$EXPECTED_MAIN" "$TARGET_COMMIT"
printf 'Type target commit %s to confirm: ' "$TARGET_COMMIT"
read -r CONFIRM_COMMIT
if test "$CONFIRM_COMMIT" != "$TARGET_COMMIT"; then
  printf 'Recovery cancelled.\n' >&2
  exit 1
fi
git push --force-with-lease="refs/heads/main:$EXPECTED_MAIN" sshdock "$TARGET_COMMIT:refs/heads/main"
```

Copy the new queued deployment ID from this push (stdout or stderr), then verify that exact attempt, service health and the public response separately:

```bash
RECOVERY_ATTEMPT=dep_replace_with_the_new_queued_id
ssh sshdock@sshdock.example.com deployments info failed-deploy-and-git-recovery "$RECOVERY_ATTEMPT" --json
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://failed-deploy-and-git-recovery.example.com
sudo sshdock apps health failed-deploy-and-git-recovery
sudo sshdock releases list failed-deploy-and-git-recovery
sudo sshdock deployments list failed-deploy-and-git-recovery
sudo sshdock events list failed-deploy-and-git-recovery
```

Require the new attempt's commit to equal `GOOD_COMMIT` and its status to be `succeeded`; the earlier successful attempt for that commit is insufficient. The recovered application serves the official Next.js starter. The deployment list retains the failed `BAD_COMMIT` attempt and later recovery attempt. Recovery selects a Git commit or tag as remote `main`; there is no `apps rollback` command. If recovery fails, inspect its new failed ID and correct the prerequisite before explicitly redeploying current `main`.

## Cleanup

```bash
sudo sshdock apps remove failed-deploy-and-git-recovery --force
cd ..
rm -rf failed-deploy-and-git-recovery
```

App removal preserves Docker volumes by design. This stateless Next.js probe declares none.

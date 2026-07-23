# Failed deploy and Git recovery feature lab

## Purpose

This lab reuses the Next.js framework compatibility probe to show the difference between accepting a Git ref and completing a healthy deployment. Its only overlay changes the copied probe's Compose build to name an absent Dockerfile. That creates a controlled build failure without copying or changing the canonical probe, its Dockerfile, or the generated application source.

Replace `example.com` below with the SSHDock base domain.

## Deploy a known-good commit

Copy the Next.js deployment envelope and this one-file failure overlay from `main` until a release tag contains the lab:

```bash
mkdir failed-deploy-and-git-recovery
cd failed-deploy-and-git-recovery
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/nextjs
curl -fsSL https://raw.githubusercontent.com/sshdock/sshdock/main/examples/labs/failed-deploy-and-git-recovery/failure.patch \
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

Compare the remote branch with `BAD_COMMIT`, then inspect the supported SSHDock surfaces:

```bash
git ls-remote sshdock refs/heads/main
sudo sshdock apps health failed-deploy-and-git-recovery
sudo sshdock releases list failed-deploy-and-git-recovery
sudo sshdock deployments list failed-deploy-and-git-recovery
sudo sshdock events list failed-deploy-and-git-recovery
```

`refs/heads/main` resolves to `BAD_COMMIT` even though the deployment failed. Health and deployment history show the failed build attempt with actionable detail; release history retains both commits; events include `git.ref_accepted`, `deploy.started`, and `deploy.failed`. A failed deploy is not an SSHDock rollback operation.

## Recover through remote main

Push the saved good commit to remote `main`, then verify that it is serving again while the failure remains in history:

```bash
git push --force sshdock "$GOOD_COMMIT:main"
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://failed-deploy-and-git-recovery.example.com
sudo sshdock apps health failed-deploy-and-git-recovery
sudo sshdock releases list failed-deploy-and-git-recovery
sudo sshdock deployments list failed-deploy-and-git-recovery
sudo sshdock events list failed-deploy-and-git-recovery
```

The recovered application serves the official Next.js starter. The deployment list shows the failed `BAD_COMMIT` attempt and the later successful push of `GOOD_COMMIT`; the failure evidence remains available for inspection. Recovery works by selecting a known-good Git commit or tag and pushing it to remote `main`, not through an `apps rollback` command.

## Cleanup

```bash
sudo sshdock apps remove failed-deploy-and-git-recovery --force
cd ..
rm -rf failed-deploy-and-git-recovery
```

App removal preserves Docker volumes by design. This stateless Next.js probe declares none.

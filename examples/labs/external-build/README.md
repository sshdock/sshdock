# External-build feature lab

This lab applies one Compose patch to the [Gin compatibility probe](../../frameworks/gin/README.md). GitHub Actions tests the pinned official source, builds and smoke-tests its image, publishes a full-commit GHCR tag, then pushes that exact commit to SSHDock. There is no application source fork or target-side build.

Read [External builds](../../../docs/EXTERNAL_BUILDS.md) for the provider-neutral contract, credentials and recovery boundary. Install a server containing `SSHDOCK_GIT_SHA`; v0.3.1 is too old for this lab.

## Prepare the application repository

Copy the canonical envelope, apply the overlay, and install the complete workflow:

```bash
mkdir gin-external
cd gin-external
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/gin
curl -fsSL https://raw.githubusercontent.com/sshdock/sshdock/main/examples/labs/external-build/external-build.patch \
  -o external-build.patch
curl -fsSL https://raw.githubusercontent.com/sshdock/sshdock/main/examples/labs/external-build/deploy.yml \
  -o deploy.yml
git init -b main
git apply external-build.patch
mkdir -p .github/workflows
mv deploy.yml .github/workflows/deploy.yml
rm external-build.patch
```

Edit `compose.yml`: replace `ghcr.io/example/gin-external` with your lowercase GHCR namespace/image. The workflow reads this same Compose reference. Port `18120` must be free on the VPS. The Dockerfile, pinned source, health check, runtime user and restart policy remain unchanged. `APP_REVISION` exposes public commit metadata to the container.

Create your own GitHub repository and configure its Actions settings:

| Setting | Value |
| --- | --- |
| Variable `SSHDOCK_HOST` | Your SSH hostname, for example `sshdock.example.com` |
| Variable `SSHDOCK_HEALTH_URL` | `https://gin-external.example.com/ping` |
| Secret `SSHDOCK_DEPLOY_KEY` | A dedicated private SSH key authorized by `sshdock ssh-keys add` |
| Secret `SSHDOCK_KNOWN_HOSTS` | The independently verified host public-key entry for that hostname |

Configure the server's base domain and DNS normally. This example uses SSH port 22, app `gin-external`, and an amd64 VPS. For arm64 use a matching runner and smoke-test platform. If you rename the app, update the workflow's app name, concurrency group, public URL and host port.

GHCR packages [start private](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry). Before deployment, either configure a `read:packages` token for the server's `sshdock` user, or publish the package and make it public. In the latter case the first pull may fail; after changing visibility run `apps redeploy gin-external`. The workflow publishes with `GITHUB_TOKEN` and `packages: write`; never copy that ephemeral token to the VPS.

## Deploy and verify

```bash
git add .
git commit -m "Deploy Gin using external builds"
git remote add origin git@github.com:YOUR_OWNER/YOUR_REPOSITORY.git
git push -u origin main
```

The workflow checks out full history at the triggering SHA, tests `./basic` in the Dockerfile's source stage, builds and smoke-tests `/ping`, publishes, then pushes the exact commit. Reruns reuse an already published image. Keep commit tags unchanged and retain them for recovery. The workflow does not force-push or mutate app config.

It follows durable logs, then requires healthy status, a succeeded attempt for the expected SHA, matching current `main`, matching `APP_REVISION`, and HTTPS `/ping` returning `pong`. This rejects a failed deployment even if an older container still serves HTTP.

```bash
ssh sshdock@sshdock.example.com apps health gin-external
ssh sshdock@sshdock.example.com deployments list gin-external
ssh sshdock@sshdock.example.com deployments logs gin-external
ssh sshdock@sshdock.example.com apps exec gin-external web -- printenv APP_REVISION
curl -fsS https://gin-external.example.com/ping
```

The VPS log must contain no application build. Commit a second envelope change and push `origin main` again: `APP_REVISION` must change to that SHA. The unchanged upstream Gin probe still returns `pong`; the automated local-registry test additionally uses different baked image bodies to verify artifact selection independently of environment metadata.

## Operate, recover, and clean up

Normal lifecycle commands work without CI-side tag settings. The stateless Gin probe has no volumes; its README documents the upstream demonstration-only credentials and in-memory data limitations.

If deployment failed after publication, inspect the recorded failure, fix it and retry current `main`:

```bash
ssh sshdock@sshdock.example.com apps redeploy gin-external
```

Keep one deployment workflow per app. [GitHub concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency) serializes this workflow but can replace pending runs; it does not promise every intermediate commit will deploy. SSHDock's busy-app and Git fast-forward checks remain authoritative. Only intentional recovery should force-push an older commit, whose image must still exist.

```bash
ssh sshdock@sshdock.example.com apps remove gin-external --force
```

Removing the app does not delete your GitHub repository, package versions or CI secrets.

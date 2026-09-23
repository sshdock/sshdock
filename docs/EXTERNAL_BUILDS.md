# External builds

SSHDock has one Git/Compose deployment contract and two places to build:

- **Local Build:** push directly to SSHDock; Compose builds `build:` services on the VPS.
- **External Build:** your CI tests and builds an image, publishes it under the full source commit, then pushes that exact commit to SSHDock. Compose pulls `image:` services on the VPS.

External Build requires a server version containing `SSHDOCK_GIT_SHA`; v0.3.1 does not provide it. Until the next release, this is a source-build feature. The [GitHub Actions and GHCR lab](../examples/labs/external-build/README.md) is a complete example using the existing Gin probe. Any CI and OCI registry can follow the same contract.

## Commit selects the image

Commit an image-only Compose service:

```yaml
services:
  web:
    image: registry.example/owner/app:${SSHDOCK_GIT_SHA:?deploy a commit}
    ports:
      - "127.0.0.1:3000:8080"
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/health"]
      interval: 5s
      timeout: 3s
      retries: 12
```

Use your application's real port and health command; the image must contain that command. In CI, check out and test the intended commit, then:

```bash
git_sha=$(git rev-parse HEAD)
image="registry.example/owner/app:$git_sha"
docker build -t "$image" .
# Smoke-test the built image before publishing.
docker push "$image"
git push sshdock "$git_sha:refs/heads/main"
```

Build for the VPS architecture. An amd64 runner's default image does not support arm64; use a matching runner or standard multi-platform build tooling. Do not change the working tree between the tested build and push.

The daemon supplies the accepted attempt's full SHA to Compose. In the existing `pull → build → up --wait` sequence, `build` has nothing to build for an image-only model. A Dockerfile may remain in Git for CI; the absence of `build:` in the effective Compose model determines target-side behavior. Mixed models still build their `build:` services on the VPS.

`SSHDOCK_GIT_SHA` overrides inherited process and `.env` values and is never stored app config. Operational commands use the current checked-out Compose revision, including after a failed deployment. Do not call `config set IMAGE_TAG` or generate release overrides. See [the metadata decision](adr/0001-git-revision-compose-metadata.md).

## Credentials and artifact retention

Public images need no registry credentials on the server. For private images, a server administrator configures Docker authentication for the `sshdock` Unix user:

```bash
# Feed a read-only registry token through stdin from your secret source.
printf '%s' "$REGISTRY_READ_TOKEN" \
  | sudo -u sshdock -H docker login registry.example -u registry-user --password-stdin
```

This is host administration. Deployment CI uses only restricted Git/operator SSH and never needs a root shell. Store its private key in the CI secret store, authorize the public key with `sshdock ssh-keys add`, and pin an independently verified host key in `known_hosts`. Deploy keys are trusted-owner credentials for all apps on this single-owner server, not an app isolation boundary.

Docker stores registry credentials under that user's home; use a supported credential helper where available. These are host administration state, separate from SSHDock's encrypted application config. Runtime app secrets remain on the VPS. Do not put them in Git, image layers, build arguments, or CI's build environment.

The registry owner must keep commit tags unchanged and retain images needed for redeploy/recovery. A SHA-shaped tag remains mutable: SSHDock deterministically selects the tag but cannot prevent its owner from replacing the bytes. Avoid rebuilding published commits and use registry controls. SSHDock provides no managed registry, artifact-retention service, or tag-to-digest database.

## Completion, concurrency, and recovery

A successful Git exit means the ref was accepted. Attached output normally follows the daemon, but a failed deployment does not undo the Git update. CI must also inspect the recorded attempt and health; an HTTP success alone could come from an older container.

```bash
ssh sshdock@server deployments logs my-app -f
ssh sshdock@server deployments list my-app
ssh sshdock@server apps health my-app
```

Require a succeeded latest attempt for the expected full commit, matching current `main`, healthy services, and public HTTPS. `deployments list` is tab-separated: ID, status, trigger, commit, release, start, finish, failure stage/detail, retry guidance. Health and log commands can exit successfully while reporting an unhealthy deployment, so inspect their content. The complete workflow demonstrates these checks.

Serialize CI jobs per app and disable cancellation of in-progress deployment jobs. SSHDock independently rejects a second pending/active same-app push before changing its ref. Git rejects stale non-fast-forward updates after a newer commit; CI must never force-push around that rejection. A queued CI job need not run in source order. Distinct commits select distinct tags without shared image-tag config mutations.

Missing tags, pull credentials, network access, or target-platform images produce recorded pull failures and retry guidance. After an external fix, `apps redeploy my-app` retries current remote `main`; pushing an unchanged ref creates no attempt. Pull failures leave existing containers in place, but later Compose replacement failures have no zero-downtime or automatic rollback guarantee.

For intentional Git recovery, verify the retained image, then explicitly force-push the older commit to remote `main`. That commit selects both the Compose model and image tag. Both modes share release history, redacted logs, config, routing and recovery commands.

## Acceptance

`make external-build-e2e` uses a disposable local OCI registry and real Docker/OpenSSH, without GHCR credentials. It publishes two distinct image bodies before deploying either, removes local image tags to require pulls, and checks each served body and container image. It also verifies `.env` spoof protection, restricted `exec`/`run`, missing-image failure, Git recovery and redeploy. The target has no Dockerfile and its log reports no services to build.

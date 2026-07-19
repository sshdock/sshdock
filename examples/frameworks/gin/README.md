# Gin framework compatibility probe

## Purpose

This probe builds Gin's unmodified official `examples/basic` service inside the image and deploys its compiled production binary through SSHDock. It is point-in-time compatibility evidence, not editable starter source or a Gin tutorial.

Create a real user-owned application with the [official Gin quickstart](https://gin-gonic.com/en/docs/quickstart):

```bash
mkdir example-app
cd example-app
go mod init example-app
go get github.com/gin-gonic/gin
```

## Prerequisites

- A working SSHDock server with a base domain configured.
- DNS for `*.example.com` pointing at the server.
- Your public key added to the `sshdock` operator account.
- Local `curl`, `git`, and `tar` commands.

Replace `example.com` below with the server's base domain.

## Topology

The root `compose.yml` builds one `web` service. The Dockerfile checks out the official Gin examples repository at an exact commit and compiles its `basic` package in separate source and build stages. The final Alpine image contains only the stripped binary and runtime, runs as unprivileged UID 65532, and never fetches source or invokes Go tooling at startup.

Compose publishes port `8080` only on `127.0.0.1:18103`, checks the official `/ping` route, and applies `restart: unless-stopped`.

## Pinned versions

- Official source: [`gin-gonic/examples@70ea0357aca8fab6638a85709ff74d51c1bb0e73`](https://github.com/gin-gonic/examples/tree/70ea0357aca8fab6638a85709ff74d51c1bb0e73/basic)
- Source dependency: `github.com/gin-gonic/gin v1.10.1` from the pinned upstream `go.mod`
- Builder: `golang:1.26.5-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc`
- Runtime: `alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659`

The source commit and multi-platform image manifests are immutable inputs. Go module proxy availability can still change, so this is a tested compatibility claim for this SSHDock commit, not a promise of bit-for-bit rebuilds forever.

## Deploy

Until a release tag contains this probe, copy its three-file envelope from `main`:

```bash
mkdir gin
cd gin
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/gin
git init -b main
git add .
git commit -m "Deploy Gin"
git remote add sshdock git@sshdock.example.com:gin.git
git push sshdock main
```

The first push creates the app, builds the official service, waits for `/ping`, and creates the default route when the server has a base domain.

## Verify

```bash
curl -I http://gin.example.com/ping
curl -fsS https://gin.example.com/ping
curl -fsS https://gin.example.com/user/alice
sudo sshdock apps health gin
sudo sshdock domains check gin
sudo sshdock deployments list gin
sudo sshdock events list gin
```

The HTTPS `/ping` response is `pong`; the official user route returns a JSON response with `status: no value` until its in-memory state is changed.

## Operate

```bash
sudo sshdock logs gin web --tail 100
sudo sshdock apps restart gin
sudo sshdock apps health gin
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://gin.example.com/ping
sudo sshdock apps redeploy gin
```

## Upgrade

Choose a tested Gin examples commit and supported Go and Alpine images. Update the three Dockerfile arguments and recorded digests, inspect the pinned upstream `basic` source and `go.mod`, then build the complete probe before pushing:

```bash
docker compose build --pull
docker compose up --wait
curl -fsS http://127.0.0.1:18103/ping
git add Dockerfile README.md
git commit -m "Upgrade Gin probe"
git push sshdock main
```

Do not commit the upstream source tree, module manifests, module caches, or build output.

## Cleanup

```bash
sudo sshdock apps remove gin --force
```

## Persistence

The official basic service keeps its example user values in process memory. The probe declares no volumes, so restart, redeploy, and removal discard that transient state and require no volume cleanup.

## Limitations

This probe proves the official `/ping` and user routes, a compiled production binary, health, logs, restart, redeploy, and HTTPS routing. It does not demonstrate durable storage, authentication suitable for production, background jobs, zero-downtime deployment, or Gin application development.

## Security boundaries

The public HTTP port binds to IPv4 loopback so Caddy remains the external HTTP and HTTPS entry point. The final container runs as unprivileged UID 65532 and contains neither Git nor the Go toolchain. The upstream basic example includes demonstration-only basic-auth credentials and in-memory state; do not treat them as production security design. SSHDock accepts trusted-owner Compose files and does not sandbox malicious images or application code.

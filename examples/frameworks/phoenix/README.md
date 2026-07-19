# Phoenix LiveView framework compatibility probe

## Purpose

This probe generates the smallest database-backed official Phoenix LiveView resource inside the image and deploys its production release through SSHDock. It is point-in-time compatibility evidence, not editable application source or a Phoenix tutorial.

Create a real user-owned application with the [official Phoenix generator workflow](https://hexdocs.pm/phoenix/Mix.Tasks.Phx.New.html):

```bash
mix archive.install hex phx_new
mix phx.new example_app
```

## Prerequisites

- A working SSHDock server with a base domain configured.
- DNS for `*.example.com` pointing at the server.
- Your public key added to the `sshdock` operator account.
- Local `curl`, `git`, `openssl`, and `tar` commands, plus a browser for LiveView verification.

Replace `example.com` below with the server's base domain.

## Topology

The root `compose.yml` builds one `web` service. The Dockerfile runs `phx_new 1.8.9`, generates the SQLite application, and uses the official `phx.gen.live` task to add one `Item` resource with one `name` field. It adds exactly the four routes printed by that generator. No application source or dependency manifest is authored or committed.

Intermediate stages fetch dependencies, compile the application, build production assets, and assemble an official Elixir release. The final Alpine image contains only that release and its runtime libraries, runs as unprivileged UID 65534, migrates SQLite before startup, and never runs Mix, the Phoenix generator, or a development server.

Compose publishes port `4000` only on `127.0.0.1:18104`, requires Phoenix's signing secret and real HTTPS hostname through SSHDock config, persists SQLite in `phoenix_data`, checks the generated `/items` LiveView, and applies `restart: unless-stopped`.

## Pinned versions

- Generator and Phoenix source: `phx_new 1.8.9`
- LiveView resolved during this verification: `Phoenix LiveView 1.2.7`
- Builder: `hexpm/elixir:1.20.2-erlang-28.5.0.3-alpine-3.23.5@sha256:6f03034e254126f063959873d8d3b811ee92abaabab27b62c53982c4a1034e39`
- Runtime: `alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40`

The generator and multi-platform image manifests are immutable inputs. Hex, GitHub, and generated transitive dependency resolution remain external build inputs, so this is a tested compatibility claim for this SSHDock commit, not a promise of bit-for-bit rebuilds forever.

## Deploy

Until a release tag contains this probe, copy its three-file envelope from `main`:

```bash
mkdir phoenix
cd phoenix
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/phoenix
git init -b main
git add .
git commit -m "Deploy Phoenix LiveView"
git remote add sshdock git@sshdock.example.com:phoenix.git
git push sshdock main
```

The accepted push creates the app and stops safely at the required `SECRET_KEY_BASE`. Store a generated secret, redeploy current remote `main`, and attach the conventional domain explicitly because the first deployment did not reach Compose success:

```bash
openssl rand -base64 48 \
  | ssh sshdock@sshdock.example.com config set phoenix SECRET_KEY_BASE
printf '%s' 'phoenix.example.com' \
  | ssh sshdock@sshdock.example.com config set phoenix PHX_HOST
ssh sshdock@sshdock.example.com config list phoenix
sudo sshdock apps redeploy phoenix
sudo sshdock domains attach phoenix web phoenix.example.com --port 18104
```

## Verify

Verify routing, the generated LiveView, and operator state:

```bash
curl -I http://phoenix.example.com/items
curl -fsS https://phoenix.example.com/items
sudo sshdock apps health phoenix
sudo sshdock domains check phoenix
sudo sshdock deployments list phoenix
sudo sshdock events list phoenix
```

Open `https://phoenix.example.com/items` in a browser, choose **New Item**, enter `before-restart`, and save it. The generated form and navigation update through a secure WebSocket. Keep the tab open, restart the app with the command below, observe the generated **Attempting to reconnect** state clear, then create `after-restart`. Both rows remain visible after reconnection because SQLite uses the named volume. The opt-in `make phoenix-liveview-e2e` target automates this same browser, restricted-restart, reconnect, and second-update flow against a deployed probe.

## Operate

Inspect bounded logs, restart the Compose service while the LiveView tab remains open, and verify recovery:

```bash
sudo sshdock logs phoenix web --tail 100
sudo sshdock apps restart phoenix
sudo sshdock apps health phoenix
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://phoenix.example.com/items
```

To rebuild and deploy the same Git commit explicitly:

```bash
sudo sshdock apps redeploy phoenix
```

## Upgrade

Choose a tested `phx_new` release and matching supported Elixir, Erlang, and Alpine images. Update the three Dockerfile arguments and recorded digests, then build and exercise the generated LiveView before pushing:

```bash
docker compose build --pull
SECRET_KEY_BASE="$(openssl rand -base64 48)" PHX_HOST=phoenix.example.com docker compose up --wait
curl -fsS http://127.0.0.1:18104/items
git add Dockerfile README.md
git commit -m "Upgrade Phoenix LiveView probe"
git push sshdock main
```

Review the generated application and official Phoenix release notes when changing versions. Do not commit generated source, dependency manifests, lockfiles, caches, or build output.

## Cleanup

Ordinary removal deletes SSHDock metadata, routes, and Compose containers:

```bash
sudo sshdock apps remove phoenix --force
```

## Persistence

The `phoenix_data` volume persists the generated SQLite database across restart, redeploy, and app removal. Delete the volume separately through server administration only when the generated Item records are intentionally disposable.

## Limitations

This probe proves an official generated LiveView resource, production assets and release, HTTPS, secure WebSocket interaction, reconnect after a supported restart, SQLite persistence, health, logs, and redeploy. It does not demonstrate external databases, authentication, background jobs, clustering, zero-downtime deployment, or Phoenix application development.

## Security boundaries

The public HTTP port binds to IPv4 loopback so Caddy remains the external HTTP and HTTPS entry point. `SECRET_KEY_BASE` is required from SSHDock's encrypted config rather than committed. The final container runs as UID 65534 and contains neither Mix, Git, nor generated source. SSHDock accepts trusted-owner Compose files and does not sandbox malicious images or application code.

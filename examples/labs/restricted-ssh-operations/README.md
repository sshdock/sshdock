# Restricted SSH operations feature lab

## Purpose

This lab reuses the Laravel framework compatibility probe to exercise SSHDock's restricted operator commands without copying its generated source, Dockerfile, or Compose file. Its one command fixture, `acceptance.sh`, runs only upstream Laravel Artisan commands: one against the existing service and one in a removable one-off container.

Replace `example.com` below with the SSHDock base domain.

## Deploy the Laravel probe

Copy the untouched Laravel deployment envelope from `main` until a release tag contains this lab:

```bash
mkdir restricted-ssh-operations
cd restricted-ssh-operations
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/laravel
git init -b main
git add .
git commit -m "Deploy Laravel restricted SSH operations lab"
git remote add sshdock git@sshdock.example.com:restricted-ssh-operations.git
git push sshdock main
printf 'base64:%s' "$(openssl rand -base64 32)" \
  | ssh sshdock@sshdock.example.com config set restricted-ssh-operations APP_KEY
ssh sshdock@sshdock.example.com apps redeploy restricted-ssh-operations
ssh sshdock@sshdock.example.com apps health restricted-ssh-operations
```

The first push records the expected missing-`APP_KEY` deployment failure before Compose starts. `config set` does not deploy; `apps redeploy` retries that same remote `main` commit through the restricted operator surface.

## Run the executable overlay

Set the restricted operator target, a normal server-administrator target for route and volume checks, and an app hostname. The hostname must already resolve to the server if you also want to verify it from outside the host.

```bash
SSHDOCK_TARGET=sshdock@sshdock.example.com \
SSHDOCK_ADMIN_TARGET=admin@example.com \
SSHDOCK_ROUTE_HOST=restricted-ssh-operations.example.com \
bash acceptance.sh
```

Set `SSHDOCK_IDENTITY_FILE=/path/to/key` too when SSH does not already select the operator and administrator key. The script drives the lifecycle, exec, run, PTY, route, rejection, removal, and volume assertions below. It removes the app but intentionally leaves `laravel_storage` so the administrator can decide when to delete persisted data.

## Lifecycle operations

The executable overlay runs each whole-app lifecycle command through the operator account:

```bash
ssh sshdock@sshdock.example.com apps stop restricted-ssh-operations
ssh sshdock@sshdock.example.com apps health restricted-ssh-operations
ssh sshdock@sshdock.example.com apps start restricted-ssh-operations
ssh sshdock@sshdock.example.com apps restart restricted-ssh-operations
ssh sshdock@sshdock.example.com apps redeploy restricted-ssh-operations
ssh sshdock@sshdock.example.com apps health restricted-ssh-operations
```

`stop`, `start`, and `restart` operate on the existing Compose containers; they do not apply changed Compose files or stored config. `redeploy` checks out and deploys current remote `main`, so use it after a Git push or config change. The final health command reports the running `web` service and the deployment history retains the initial failed attempt plus each successful redeploy.

## Existing-service and one-off commands

`apps exec` targets a running service container. A normal SSH session is non-PTY and SSHDock adds Compose `-T`, which is appropriate for scripts:

```bash
ssh sshdock@sshdock.example.com apps exec restricted-ssh-operations web -- php artisan about
ssh sshdock@sshdock.example.com apps run restricted-ssh-operations web -- php artisan migrate --force
ssh sshdock@sshdock.example.com apps exec restricted-ssh-operations web -- php artisan migrate:status
```

`apps run` creates a one-off container with `--rm`; the migration uses Laravel's declared storage volume but the container does not remain after it exits. The required `--` keeps the command argv separate from SSHDock and Compose options.

For a terminal-aware command, request a PTY explicitly. SSHDock then permits Compose to allocate the container TTY:

```bash
ssh -tt sshdock@sshdock.example.com apps exec restricted-ssh-operations web -- php artisan about
```

Use a non-PTY invocation for automation. Use `ssh -tt` only when the container command needs an interactive terminal.

## Restricted boundary

The operator account dispatches only supported SSHDock commands and never invokes a host shell. This attempt to run a host command must fail with the operator's rejection message:

```bash
if ssh -T sshdock@sshdock.example.com hostname; then
  echo "unexpected host command success" >&2
  exit 1
fi
```

Likewise, commands without the required `--`, supplied Compose flags, missing services, and stopped service containers are rejected. `apps exec` may intentionally run a shell _inside the selected container_, but it cannot escape to the host.

## Cleanup

```bash
ssh sshdock@sshdock.example.com apps remove restricted-ssh-operations --force
```

Removal clears SSHDock app state, its Git repository and worktree, Compose containers, and routes. The script verifies that the app is no longer inspectable, the prior route was active before removal, and `laravel_storage` still exists afterward. Delete that volume separately through normal server administration only when its Laravel storage and SQLite data are disposable.

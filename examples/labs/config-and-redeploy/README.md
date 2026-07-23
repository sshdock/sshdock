# Config and redeploy feature lab

## Purpose

This lab reuses the NestJS framework compatibility probe to prove SSHDock's flat encrypted config workflow. The executable overlay is one Git patch: it adds a required `CONFIG_LAB_SECRET` Compose interpolation without copying or changing the canonical probe in this repository. The generated NestJS application does not read or return the value, so the value can safely be treated as a secret.

Replace `example.com` below with the SSHDock base domain.

## Deploy the overlay

Copy the NestJS deployment envelope and this lab's one-file overlay from `main` until a release tag contains the lab:

```bash
mkdir config-and-redeploy
cd config-and-redeploy
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/frameworks/nestjs
curl -fsSL https://raw.githubusercontent.com/sshdock/sshdock/main/examples/labs/config-and-redeploy/config.patch \
  -o config.patch
git init -b main
git apply config.patch
git add .
git commit -m "Deploy NestJS config lab"
git remote add sshdock git@sshdock.example.com:config-and-redeploy.git
git rev-parse HEAD
git push sshdock main
```

The accepted Git update creates the app and records a failed deployment attempt before Docker Compose starts because `CONFIG_LAB_SECRET` is absent. The pushed commit remains remote `main`; do not create another commit for the rest of this lab.

## Configure without an implicit deployment

Use distinct demo values so the redaction checks are unambiguous. `config set` and `config import` record config state only: they do not restart or redeploy the app.

```bash
printf '%s\n' 'config-lab-secret-value' \
  | ssh sshdock@sshdock.example.com config set config-and-redeploy CONFIG_LAB_SECRET
printf '%s\n' 'CONFIG_LAB_TEMP=config-lab-temporary-value' \
  | ssh sshdock@sshdock.example.com config import config-and-redeploy
ssh sshdock@sshdock.example.com config list config-and-redeploy
ssh sshdock@sshdock.example.com config keys config-and-redeploy
ssh sshdock@sshdock.example.com config get config-and-redeploy CONFIG_LAB_SECRET
ssh sshdock@sshdock.example.com config unset config-and-redeploy CONFIG_LAB_TEMP
sudo sshdock deployments list config-and-redeploy
```

`config list` shows keys but redacts values. `config keys` shows only names. `config get` intentionally reveals the requested value, so use it only when that value may appear in your terminal history. Before the explicit redeploy, `deployments list` should still show only the failed attempt from the first push.

## Redeploy and verify

Redeploy retries the existing remote `main` commit and adds a new deployment attempt for that same commit:

```bash
sudo sshdock apps redeploy config-and-redeploy
curl -fsS https://config-and-redeploy.example.com
sudo sshdock apps health config-and-redeploy
sudo sshdock deployments list config-and-redeploy
sudo sshdock events list config-and-redeploy
sudo sshdock logs config-and-redeploy web --tail 100
```

The HTTPS response is the official NestJS starter response:

```text
Hello World!
```

The health, deployment, event, and normal log output must not contain either demo value. The deployment list contains one failed config-gated attempt and one successful `redeploy` attempt for the same commit.

## Cleanup

```bash
sudo sshdock apps remove config-and-redeploy --force
cd ..
rm -rf config-and-redeploy
```

App removal preserves Docker volumes by design. This stateless NestJS probe declares none.

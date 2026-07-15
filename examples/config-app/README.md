# Config App

Minimal Python example that declares required config with native Compose interpolation and passes it into the container as `APP_MESSAGE`. SSHDock should infer `web:18082` from `127.0.0.1:18082:8080`.

For the fuller walkthrough, see [`../../docs/EXAMPLES.md`](../../docs/EXAMPLES.md).

This example renders `APP_MESSAGE` publicly so it should be a non-secret demo value. Do not return real secrets from an app response.

## Deploy

Replace `example.com` with your SSHDock base domain.

```bash
mkdir config-app
cd config-app
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/Dockerfile
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/server.py
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/README.md
git init -b main
git add .
git commit -m "Deploy config app"
git remote add sshdock git@sshdock.example.com:config-app.git
git push sshdock main
```

The first push is expected to create the app and fail before Docker Compose starts:

```text
required variable APP_MESSAGE is missing a value
```

Set the missing value over SSH. `config list` stays redacted, while `config get` is an explicit reveal:

```bash
printf '%s\n' 'Hello from SSHDock config' | ssh sshdock@sshdock.example.com config set config-app APP_MESSAGE
ssh sshdock@sshdock.example.com config list config-app
ssh sshdock@sshdock.example.com config get config-app APP_MESSAGE
```

Create a new commit so Git runs the receive hook, then push again:

```bash
git commit --allow-empty -m "Deploy with config"
git push sshdock main
```

## Verify

```bash
sudo sshdock apps list
sudo sshdock domains list config-app
sudo sshdock events list config-app
sudo sshdock logs config-app web
ssh -T sshdock@sshdock.example.com
```

```bash
curl -I http://config-app.example.com
curl -fsS https://config-app.example.com
```

Expected: `config-app` is healthy, `config-app.example.com` routes to `web:18082`, HTTP redirects to HTTPS, and HTTPS contains `SSHDock config example: Hello from SSHDock config`.

## Clean Up

```bash
sudo sshdock apps remove config-app --force
sudo sshdock apps list
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_config-app-' || true
```

```bash
cd ..
rm -rf config-app
```

Expected: `config-app` no longer appears, no `sshdock_config-app-*` containers remain. No Docker volumes need to be removed for this config example.

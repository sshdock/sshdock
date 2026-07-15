# Build Service

Minimal Python service built by Docker Compose. SSHDock should infer `web:18083` from `127.0.0.1:18083:8080`.

For the fuller walkthrough, see [`../../docs/EXAMPLES.md`](../../docs/EXAMPLES.md).

## Deploy

Replace `example.com` with your SSHDock base domain.

```bash
mkdir build-service
cd build-service
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/Dockerfile
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/server.py
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/README.md
git init -b main
git add .
git commit -m "Deploy build service"
git remote add sshdock git@sshdock.example.com:build-service.git
git push sshdock main
```

## Verify

```bash
sudo sshdock apps list
sudo sshdock domains list build-service
sudo sshdock events list build-service
sudo sshdock logs build-service web
ssh -T sshdock@sshdock.example.com
```

```bash
curl -I http://build-service.example.com
curl -fsS https://build-service.example.com
```

Expected: `build-service` is healthy, `build-service.example.com` routes to `web:18083`, HTTP redirects to HTTPS, and HTTPS contains `SSHDock build service OK`.

## Clean Up

```bash
sudo sshdock apps remove build-service --force
sudo sshdock apps list
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_build-service-' || true
```

```bash
cd ..
rm -rf build-service
```

Expected: `build-service` no longer appears, no `sshdock_build-service-*` containers remain. No Docker volumes need to be removed for this build example.

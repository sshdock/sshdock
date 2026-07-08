# Web, Worker, Redis Example

This example proves a common SaaS shape: one routed web service, one background worker, and one private Redis service.

Deploy:

```bash
mkdir web-worker-redis
cd web-worker-redis
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/web-worker-redis/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/web-worker-redis/Dockerfile.worker
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/web-worker-redis/worker.sh
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/web-worker-redis/README.md
mkdir public
curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/main/examples/web-worker-redis/public/index.html
git init -b main
git add .
git commit -m "Deploy web worker redis"
git remote add sshdock git@sshdock.example.com:web-worker-redis.git
git push sshdock main
```

Verify:

```bash
sudo sshdock apps list
sudo sshdock domains list web-worker-redis
sudo sshdock logs web-worker-redis web
sudo sshdock logs web-worker-redis worker
curl -fsS https://web-worker-redis.example.com
ssh -T dashboard@sshdock.example.com
```

Expected evidence:

- `apps list` shows `web-worker-redis healthy local`.
- `domains list web-worker-redis` includes `web-worker-redis.example.com`, service `web`, and port `18084`.
- HTTPS returns `SSHDock web worker Redis OK`.
- Worker logs show Redis `PONG` output.
- Redis has no public route.

## Clean Up

```bash
sudo sshdock apps remove web-worker-redis --force
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_web-worker-redis-' || true
```

No Docker volumes need to be removed.

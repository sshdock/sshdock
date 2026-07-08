# Worker Only Example

This example proves an app can run background work without a public HTTP route.

Deploy:

```bash
mkdir worker-only
cd worker-only
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/worker-only/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/worker-only/Dockerfile
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/worker-only/worker.sh
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/worker-only/README.md
git init -b main
git add .
git commit -m "Deploy worker only"
git remote add sshdock git@sshdock.example.com:worker-only.git
git push sshdock main
```

Verify:

```bash
sudo sshdock apps list
sudo sshdock logs worker-only worker
sudo sshdock events list worker-only
ssh -T dashboard@sshdock.example.com
```

Expected evidence:

- `apps list` shows `worker-only healthy local`.
- `domains list worker-only` has no public route.
- `logs worker-only worker` includes `SSHDock worker-only example tick`.
- The dashboard shows the app and worker service even though no HTTPS route exists.

## Clean Up

```bash
sudo sshdock apps remove worker-only --force
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_worker-only-' || true
```

No Docker volumes need to be removed.

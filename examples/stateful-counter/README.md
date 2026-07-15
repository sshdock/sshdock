# Stateful Counter Example

This example proves named-volume persistence for a small built web service. Each request increments a counter stored under `/data` in a Docker named volume.

It is also the preferred future backup/restore demo candidate because it has simple state that is easy to verify before and after restore.

Deploy:

```bash
mkdir stateful-counter
cd stateful-counter
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/stateful-counter/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/stateful-counter/Dockerfile
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/stateful-counter/server.py
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/stateful-counter/README.md
git init -b main
git add .
git commit -m "Deploy stateful counter"
git remote add sshdock git@sshdock.example.com:stateful-counter.git
git push sshdock main
```

Verify:

```bash
curl -fsS https://stateful-counter.example.com
curl -fsS https://stateful-counter.example.com
sudo sshdock apps redeploy stateful-counter
curl -fsS https://stateful-counter.example.com
sudo sshdock logs stateful-counter web
ssh -T sshdock@sshdock.example.com
```

Expected evidence:

- The counter increases across requests.
- The counter still increases after redeploy because the `counter-data` named volume is preserved.
- `domains list stateful-counter` includes `stateful-counter.example.com`, service `web`, and port `18086`.
- The named volume remains after app removal unless you delete it manually.

## Clean Up

```bash
sudo sshdock apps remove stateful-counter --force
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_stateful-counter-' || true
sudo docker volume ls --format '{{.Name}}' | grep '^sshdock_stateful-counter_'
```

To delete the demo counter data too:

```bash
sudo docker volume rm sshdock_stateful-counter_counter-data
```

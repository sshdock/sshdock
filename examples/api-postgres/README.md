# API Postgres Example

This example proves a stateful API shape: one routed API service backed by a private Postgres database and a named volume.

The demo credentials are intentionally simple. Do not copy them into a real app.

Deploy:

```bash
mkdir api-postgres
cd api-postgres
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/api-postgres/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/api-postgres/README.md
mkdir db
curl -fsSLo db/init.sql https://raw.githubusercontent.com/sshdock/sshdock/main/examples/api-postgres/db/init.sql
git init -b main
git add .
git commit -m "Deploy API Postgres"
git remote add sshdock git@sshdock.example.com:api-postgres.git
git push sshdock main
```

Verify:

```bash
sudo sshdock apps list
sudo sshdock domains list api-postgres
sudo sshdock logs api-postgres api
sudo sshdock logs api-postgres db
curl -fsS https://api-postgres.example.com/messages
ssh -T dashboard@sshdock.example.com
```

Expected evidence:

- `apps list` shows `api-postgres healthy local`.
- `domains list api-postgres` includes `api-postgres.example.com`, service `api`, and port `18085`.
- HTTPS `/messages` returns JSON containing `SSHDock API Postgres OK`.
- The `db` service has no public route.
- The Postgres data lives in the `postgres-data` named volume.

## Clean Up

```bash
sudo sshdock apps remove api-postgres --force
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_api-postgres-' || true
sudo docker volume ls --format '{{.Name}}' | grep '^sshdock_api-postgres_'
```

The named volume remains because SSHDock preserves app data on removal. To delete the demo database too:

```bash
sudo docker volume rm sshdock_api-postgres_postgres-data
```

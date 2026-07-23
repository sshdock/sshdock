# PostgreSQL SSH-tunnel database example

## Purpose

This example runs one pinned official PostgreSQL image through SSHDock with persistent storage and no public database listener. A server administrator reaches it through an SSH tunnel; it is not a managed database service.

## Prerequisites

- A working SSHDock server and an administrator SSH account that may forward TCP connections.
- Your public key added to the restricted `sshdock` operator account for Git push and config commands.
- Local `git`, `ssh`, `openssl`, `tar`, and the official PostgreSQL `psql` client.

Replace `example.com` and `admin@example.com` below with your server domain and normal server-administrator SSH account. The restricted `sshdock` account disables TCP forwarding, so it cannot create the database tunnel.

## Topology

The root `compose.yml` runs only PostgreSQL. Its port is bound to IPv4 loopback at `127.0.0.1:18205`; it is reachable from the host and through a forwarding-capable administrator SSH session, never directly from the public network. Docker Compose creates the app-scoped private default network, owned by the SSHDock Compose project; no other service joins it.

`postgres-data` preserves the PostgreSQL data directory. `POSTGRES_DB`, `POSTGRES_USER`, and `POSTGRES_PASSWORD` are required SSHDock config values, not committed credentials.

SSHDock's current generic TCP route inference can record an HTTPS route for the single loopback port. Caddy only proxies HTTP, not the PostgreSQL protocol, so that route is not a database endpoint. Inspect and detach it if present after deployment; do not attach a database domain.

## Pinned versions

- PostgreSQL: `postgres:16.14-alpine3.22@sha256:786dab398303b8ce7cb76b407bb21ef2e4dfbbbd4c6abcf3d29b3130467ffdbc`

This is the official PostgreSQL 16 Alpine image selected for this point-in-time compatibility result. Before changing the pin, review the [PostgreSQL Docker Official Image](https://hub.docker.com/_/postgres), the [PostgreSQL release notes](https://www.postgresql.org/docs/release/), and your application's migration and backup plan.

## Deploy

Until a release tag contains this example, copy its two public files from `main`:

```bash
mkdir postgres
cd postgres
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/databases/postgres
git init -b main
git add .
git commit -m "Deploy PostgreSQL"
git remote add sshdock git@sshdock.example.com:postgres.git
git push sshdock main
```

The accepted push creates the app but stops before Compose starts because its required config is absent. Choose unique names and retain the password outside this repository, then configure and redeploy the same remote `main` commit:

```bash
printf '%s' 'app' \
  | ssh sshdock@sshdock.example.com config set postgres POSTGRES_DB
printf '%s' 'app' \
  | ssh sshdock@sshdock.example.com config set postgres POSTGRES_USER
openssl rand -base64 32 \
  | tr -d '\n' \
  | ssh sshdock@sshdock.example.com config set postgres POSTGRES_PASSWORD
ssh sshdock@sshdock.example.com config list postgres
sudo sshdock apps redeploy postgres
sudo sshdock apps health postgres
sudo sshdock domains list postgres
```

If the final command lists the inferred `postgres.example.com` HTTP route, detach it because PostgreSQL is not an HTTP service:

```bash
sudo sshdock domains detach postgres postgres.example.com
```

## Verify

Confirm on the server that Docker published PostgreSQL only to IPv4 loopback:

```bash
ssh admin@example.com 'sudo ss -ltn | grep 18205'
```

From your workstation, open a tunnel with the normal server-administrator account. Keep this command running in one terminal:

```bash
ssh -N -L 15432:127.0.0.1:18205 admin@example.com
```

In a second terminal, use the official PostgreSQL client through the tunnel. Enter the stored password only into your local shell or password manager; never commit it.

```bash
export PGPASSWORD='the value stored as POSTGRES_PASSWORD'
psql "postgresql://app@127.0.0.1:15432/app?sslmode=disable" \
  -c 'create table if not exists tunnel_proof (message text not null);'
psql "postgresql://app@127.0.0.1:15432/app?sslmode=disable" \
  -c "insert into tunnel_proof (message) values ('SSHDock tunnel proof');"
psql "postgresql://app@127.0.0.1:15432/app?sslmode=disable" \
  -c 'select message from tunnel_proof;'
unset PGPASSWORD
```

The final query returns `SSHDock tunnel proof`. This proves a real client reached PostgreSQL through the SSH tunnel; it does not expose a public TCP listener.

## Operate

```bash
sudo sshdock apps health postgres
sudo sshdock logs postgres db --tail 100
sudo sshdock apps restart postgres
sudo sshdock apps health postgres
```

Restarting the service keeps `postgres-data`. Use the same administrator SSH tunnel for database administration; SSHDock has no database console, user management, migration, or backup command.

## Upgrade

Back up the database and review PostgreSQL's major-version upgrade guidance before changing the image. Update to a newer exact version-and-digest pin, then use Git-selected deployment:

```bash
git add compose.yml README.md
git commit -m "Upgrade PostgreSQL image"
git push sshdock main
sudo sshdock apps health postgres
```

Verify the tunnel query again after a successful upgrade. A major PostgreSQL version change may require `pg_upgrade` or a dump-and-restore; SSHDock does not automate either operation.

## Cleanup

Ordinary SSHDock removal deletes app-owned metadata, routes, repositories, worktrees, and containers while preserving the database volume:

```bash
sudo sshdock apps remove postgres --force
```

Only when you intentionally want to destroy all database data, remove the volume after app removal:

```bash
sudo docker volume rm sshdock_postgres_postgres-data
```

## Persistence

`postgres-data` persists PostgreSQL data at `/var/lib/postgresql/data`. Restarts, redeploys, exact-image upgrades, and ordinary app removal preserve it. SSHDock backup inventories Docker volumes but does not copy their contents; use PostgreSQL-native backup and recovery procedures for database data.

## Limitations

This example proves one PostgreSQL instance, loopback-only host binding, SSH-tunnel connectivity, a write, a read, persistence, and cleanup. It does not provide public database hosting, SSHDock-managed users or credentials, managed migrations, application-consistent backups, replication, high availability, connection pooling, or zero-downtime upgrades.

## Security boundaries

The default listener is `127.0.0.1:18205`, so PostgreSQL is not publicly reachable unless an administrator deliberately changes the Compose port binding or host firewall. The administrator SSH account and any local user who can open the tunnel can reach the database; protect both credentials and the PostgreSQL password. SSHDock encrypts configured values at rest, but a host administrator, SSHDock process, Docker runtime, or workload with the Compose environment can read them.

Direct public TCP exposure is an advanced deployment choice, not a default. It requires PostgreSQL TLS, strong authentication, and firewall or provider allowlisting. Do not rely on Caddy for PostgreSQL TCP proxying. SSHDock's trusted-owner model does not sandbox malicious Compose workloads.

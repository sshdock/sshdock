# Shared PostgreSQL database example

## Purpose

This example runs one private PostgreSQL service and two official PostgreSQL client containers on an operator-owned external Docker network. It proves that two clients can write and read only through distinct SSHDock-stored database URLs without adding framework source or custom application code.

## Prerequisites

- A working SSHDock server and a server-administrator account that may run the explicit Docker network commands below.
- Your public key added to the restricted `sshdock` operator account for Git push, config, logs, health, and container commands.
- Local `git`, `ssh`, `openssl`, and `tar` commands.

Replace `example.com` below with the server's base domain.

## Topology

Create `sshdock-shared-postgres` once with an administrator account before deploying the app. Docker owns that external network, not SSHDock. PostgreSQL has the `shared-postgres` network alias and publishes no host port, so only containers deliberately attached to the external network can resolve and reach it.

`db`, `provision`, `client-a`, and `client-b` all use the exact official PostgreSQL Alpine image. `provision` creates the two roles and databases from the two URI-safe, hex-password connection strings. `client-a` can use only `client_a`, and `client-b` can use only `client_b`; each creates, writes, and reads its own proof table before remaining available for inspection.

`shared-postgres-data` preserves PostgreSQL data. There is no HTTP service, no Caddy route, and no public PostgreSQL listener. The shared external network is an intentional isolation tradeoff: every attached container can reach PostgreSQL at the network layer, so database roles and credentials still matter.

## Pinned versions

- PostgreSQL server, provisioner, and clients: `postgres:16.14-alpine3.22@sha256:786dab398303b8ce7cb76b407bb21ef2e4dfbbbd4c6abcf3d29b3130467ffdbc`

This is the official PostgreSQL 16 Alpine image selected for this point-in-time compatibility result. Before changing the pin, review the [PostgreSQL Docker Official Image](https://hub.docker.com/_/postgres), the [PostgreSQL release notes](https://www.postgresql.org/docs/release/), and the impact on both client databases.

## Deploy

Create the stable, operator-owned network once. Do not add `--attachable` or publish a host port unless you explicitly accept the broader access boundary.

```bash
sudo docker network create sshdock-shared-postgres
```

Until a release tag contains this example, copy its two public files from `main`:

```bash
mkdir shared-postgres
cd shared-postgres
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/databases/shared-postgres
git init -b main
git add .
git commit -m "Deploy shared PostgreSQL"
git remote add sshdock git@sshdock.example.com:shared-postgres.git
git push sshdock main
```

The accepted push creates the app but stops before Compose starts because required config is absent. Generate URI-safe hex passwords; the provisioner expects the two stored connection strings to use the shown `postgresql://user:hex-password@shared-postgres:5432/database?sslmode=disable` shape.

```bash
admin_password="$(openssl rand -hex 32)"
client_a_password="$(openssl rand -hex 32)"
client_b_password="$(openssl rand -hex 32)"
printf '%s' "$admin_password" \
  | ssh sshdock@sshdock.example.com config set shared-postgres POSTGRES_ADMIN_PASSWORD
printf 'postgresql://client_a:%s@shared-postgres:5432/client_a?sslmode=disable' "$client_a_password" \
  | ssh sshdock@sshdock.example.com config set shared-postgres CLIENT_A_DATABASE_URL
printf 'postgresql://client_b:%s@shared-postgres:5432/client_b?sslmode=disable' "$client_b_password" \
  | ssh sshdock@sshdock.example.com config set shared-postgres CLIENT_B_DATABASE_URL
unset admin_password client_a_password client_b_password
ssh sshdock@sshdock.example.com config list shared-postgres
sudo sshdock apps redeploy shared-postgres
sudo sshdock apps health shared-postgres
```

Normal config, deployment, health, and log output redacts the stored URLs and passwords. Do not attach a domain: the example intentionally has no published port.

## Verify

The initial client logs show one real write and read for each isolated database:

```bash
sudo sshdock logs shared-postgres client-a --tail 100
sudo sshdock logs shared-postgres client-b --tail 100
ssh sshdock@sshdock.example.com apps exec shared-postgres client-a -- sh -ec 'psql "$DATABASE_URL" -Atc "select message from client_a_proof"'
ssh sshdock@sshdock.example.com apps exec shared-postgres client-b -- sh -ec 'psql "$DATABASE_URL" -Atc "select message from client_b_proof"'
```

The output contains `client A proof` and `client B proof`. Client A cannot connect to Client B's database because `PUBLIC` has no `CONNECT` privilege and Client A has only the `client_a` credentials:

```bash
if ssh sshdock@sshdock.example.com apps exec shared-postgres client-a -- sh -ec '
  client_b_url="${DATABASE_URL%/client_a\?sslmode=disable}/client_b?sslmode=disable"
  psql "$client_b_url" -Atc "select 1"
'; then
  echo "client A unexpectedly connected to Client B database" >&2
  exit 1
fi
```

From the server, an official client that is not attached to the external network cannot resolve `shared-postgres`:

```bash
if sudo docker run --rm postgres:16.14-alpine3.22@sha256:786dab398303b8ce7cb76b407bb21ef2e4dfbbbd4c6abcf3d29b3130467ffdbc getent hosts shared-postgres; then
  echo "unattached client unexpectedly resolved shared-postgres" >&2
  exit 1
fi
```

That check proves deliberate network attachment, not a Docker sandbox. The default Docker bridge and the external service network are different networks.

## Operate

```bash
sudo sshdock apps health shared-postgres
sudo sshdock logs shared-postgres db --tail 100
sudo sshdock logs shared-postgres provision --tail 100
sudo sshdock apps restart shared-postgres
sudo sshdock apps health shared-postgres
```

After restart, repeat the two client reads. `restart: unless-stopped` restores the database and long-running client/provisioner containers after Docker or host restart. SSHDock does not manage the shared network, database roles, user provisioning, migrations, or a database console.

## Upgrade

Back up both client databases and review PostgreSQL's major-version upgrade guidance before changing the image. Update the one exact version-and-digest pin, then use Git-selected deployment:

```bash
git add compose.yml README.md
git commit -m "Upgrade shared PostgreSQL image"
git push sshdock main
sudo sshdock apps health shared-postgres
```

Verify both proof reads again after a successful upgrade. A major PostgreSQL version change may require `pg_upgrade` or a dump-and-restore; SSHDock does not automate either operation.

## Cleanup

Ordinary SSHDock removal deletes app-owned metadata, repositories, worktrees, and containers while preserving both the database volume and operator-owned network:

```bash
sudo sshdock apps remove shared-postgres --force
```

Only when you intentionally want to destroy both client databases, remove the project volume after app removal. Remove the shared network only after every attached application has been removed or detached:

```bash
sudo docker volume rm sshdock_shared-postgres_shared-postgres-data
sudo docker network rm sshdock-shared-postgres
```

## Persistence

`shared-postgres-data` preserves PostgreSQL data, including `client_a` and `client_b`, across restart, redeploy, exact-image upgrades, and ordinary app removal. SSHDock backup inventories Docker volumes but does not copy their contents; use PostgreSQL-native backup and recovery procedures for database data. The external network persists independently because it is operator-owned.

## Limitations

This example proves an operator-created external network, no public database port, two distinct database URLs, separate writes and reads, network non-attachment, persistence, and cleanup. It does not provide SSHDock-managed service links, external-network lifecycle, credential provisioning, database users, migrations, public TCP routing, TLS termination for PostgreSQL, application-consistent backups, replication, high availability, or zero-downtime upgrades.

## Security boundaries

PostgreSQL is reachable only by containers deliberately attached to `sshdock-shared-postgres`; it has no host or public listener. The external network is not an SSHDock feature and must be created, attached, audited, and removed by a trusted server administrator. Attached workloads can attempt TCP connections to PostgreSQL, but separate PostgreSQL roles and passwords limit database authority. SSHDock encrypts config values at rest, but a host administrator, SSHDock process, Docker runtime, or workload with access to its Compose environment can read them. SSHDock's trusted-owner model does not sandbox malicious Compose workloads.

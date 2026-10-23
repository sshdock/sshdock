# Manual application-data backup and restore

The volume commands below are an **unreleased v1.1** addition. They are local host administration, unavailable through restricted operator SSH. Use a normal administrator SSH session and the intended Docker daemon. No hosted service, schedule, storage, encryption, or recovery guarantee is included.

| Backup | Contains | Consistency boundary |
| --- | --- | --- |
| `backup create` | SSHDock database, config key, Git/worktrees, key state, Caddy files, volume inventory | Idle deployments, stopped daemon, paused operator/CI mutations; no application-data copy |
| `backup volume export` | One selected ordinary named volume's regular files/directories, numeric UID/GID and basic modes | Owner-controlled window with every writer stopped; no universal live snapshot |
| PostgreSQL `pg_dump` | One database's logical objects/data | PostgreSQL's consistent database snapshot; separate coordination for other databases/files/external services |

**All these archives are unencrypted and may contain secrets. Host-state archives include the config decryption key.** Keep archive files private, encrypt them with your own tools before external storage, protect transfer credentials, and test recovery. Checksums detect transfer damage; they do not authenticate an untrusted archive. Only restore archives you trust.

## Export a named volume

Record the app, exact source volume, Compose mapping, compatible image/version and expected data before maintenance. Ordinary app removal preserves volumes, but that does not prove a backup can recover their contents.

Pause Git pushes, CI, operator lifecycle/exec/run commands and external writers. Stop every application that writes the selected volume, including services in other Compose projects. Keep that moratorium until export finishes; deployment locks do not cover every possible writer.

```bash
ssh sshdock@sshdock.example.com apps stop my-app
# On the server, in a normal administrator session:
sudo docker volume inspect sshdock_my-app_data
sudo docker ps --filter volume=sshdock_my-app_data
sudo sshdock backup volume export sshdock_my-app_data \
  --output /root/my-app-data.tar.gz --quiesced
sudo sshdock backup volume inspect /root/my-app-data.tar.gz
```

`--quiesced` acknowledges the owner's write moratorium. The command rejects observed running read/write consumers across all containers before and after copying. It cannot detect host processes, external actors, or a writer that starts and stops between observations; the flag is not proof of database consistency. Restart the original app only after the export succeeds and the archive has been inspected.

Export accepts ordinary Docker `local` volumes without driver options. It rejects bind/NFS/plugin-backed volumes and missing sources. The output path must not exist, including as a symlink. Publication uses an atomic non-replacing hard link, so choose a local filesystem that supports it; copy/encrypt the completed archive to other storage afterward.

The helper image is pinned to `debian:trixie-20261020-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad`. It supplies GNU tar, runs without network or a writable root filesystem, and mounts the source volume read-only with `volume-nocopy`. The Docker daemon may download this public image first; no application image is built or started. See the [official image definition](https://github.com/docker-library/official-images/blob/master/library/debian).

## Inspect and restore to a new volume

```bash
sudo sshdock backup volume inspect /root/my-app-data.tar.gz
sudo sshdock backup volume restore /root/my-app-data.tar.gz \
  --name my-app-restored-data
```

Inspection reads the complete archive, including payload and gzip checksum. Restore validates and stages the complete archive before any Docker mutation. It refuses every existing destination volume, including an empty one. It creates an operation-labelled volume, verifies ownership, and extracts only into that new volume. Failed operations attempt to remove only their own helper/new volume; a cleanup failure identifies the incomplete resource for explicit administrator inspection. Interrupts cancel the operation and run bounded cleanup.

To make Compose use the restored volume, explicitly select it in the app's committed Compose model:

```yaml
services:
  web:
    volumes:
      - data:/data
volumes:
  data:
    external: true
    name: my-app-restored-data
```

Review and push that change through normal Git deployment. Restore does not copy arbitrary Docker labels, rewrite Compose, change Git refs, create containers or switch traffic. Verify the new exact deployment ID, container health, public response, known saved data, a new write, and persistence after restart. Keep the original archive until the destination passes those checks.

### Filesystem limits

The separate archive format is `sshdock-volume/v1`: bounded `manifest.json` plus a `data/` tree. It preserves regular-file bytes, empty directories, numeric UID/GID and modes `0000`–`0777`. Keep image identity and its expected numeric users compatible on the destination; host-account ownership normalization must not be applied to application volume contents.

Links, special files, special mode bits, sparse metadata and emitted ACL/xattr/SELinux extensions are rejected. Export requests GNU tar's metadata options so unsupported observable metadata is not silently discarded. Host-only attributes and flags are not guaranteed observable, preserved, or proved absent; this is not a full filesystem backup. Use application/filesystem-aware tooling if those properties matter. [GNU tar metadata options](https://www.gnu.org/software/tar/manual/tar.html), [Linux xattr visibility](https://man7.org/linux/man-pages/man2/listxattr.2.html).

Limits are a 64 KiB manifest, 100,000 unique relative paths (including inferred parent directories), 4096-byte relative paths, 16 MiB combined unique path names and 64 MiB of tar metadata/padding. Unsafe paths, duplicate/conflicting entries, unsupported records, corrupt/truncated streams and concatenated/trailing archives are rejected. Export and restore use private `0600` temporary files and require scratch space for uncompressed tar data, plus the compressed export archive. Set a suitable `TMPDIR` for export or restore when the default temporary filesystem is too small. Inspection validates without extracting application files.

Do not copy a live PostgreSQL data directory as an online backup. Use the following logical path instead.

## PostgreSQL logical dump and restore

The [PostgreSQL example](../examples/databases/postgres/README.md) runs PostgreSQL 16 with the required `POSTGRES_USER`, `POSTGRES_DB` and `POSTGRES_PASSWORD` values. Its container provides matching native clients. A custom-format dump is a consistent single-database archive while PostgreSQL remains running; coordinate application writes when consistency with files or other services is required. [PostgreSQL 16 pg_dump](https://www.postgresql.org/docs/16/app-pgdump.html).

On the workstation, use `ssh -T` and send the complete remote command as one quoted argument. The restricted operator preserves container argv; the inner shell expands credentials inside the container. Keep stderr separate from binary stdout. This creates a unique private directory and publishes the dump only after a successful transfer:

```bash
set -eu
SOURCE_OPERATOR=sshdock@source.example.com
DUMP=$(
  set -eu
  umask 077
  directory=$(mktemp -d ./postgres-backup.XXXXXX)
  partial=$directory/database.dump.partial
  trap 'rm -f "$partial"; rmdir "$directory" 2>/dev/null || :' EXIT
  trap 'exit 1' HUP INT TERM
  ssh -T "$SOURCE_OPERATOR" \
    'apps exec postgres db -- sh -ec "export PGPASSWORD=\"$POSTGRES_PASSWORD\"; exec pg_dump --host=127.0.0.1 --username=\"$POSTGRES_USER\" --dbname=\"$POSTGRES_DB\" --format=custom --no-password"' \
    </dev/null >"$partial"
  test -s "$partial"
  mv "$partial" "$directory/database.dump"
  printf '%s\n' "$directory/database.dump"
)
printf 'Backup: %s\n' "$DUMP"
```

On the destination, deploy the compatible PostgreSQL image with a fresh volume and credentials. Provision the source object owners/grantees first; for this example use the same `POSTGRES_USER`. The destination password may differ. Inspect the dump, create a distinct empty database from `template0`, then restore:

```bash
set -eu
DEST_OPERATOR=sshdock@destination.example.com
ssh -T "$DEST_OPERATOR" 'apps exec postgres db -- pg_restore --list' <"$DUMP"
ssh -T "$DEST_OPERATOR" \
  'apps exec postgres db -- sh -ec "export PGPASSWORD=\"$POSTGRES_PASSWORD\"; exec createdb --host=127.0.0.1 --username=\"$POSTGRES_USER\" --owner=\"$POSTGRES_USER\" --template=template0 --no-password sshdock_restore"'
ssh -T "$DEST_OPERATOR" \
  'apps exec postgres db -- sh -ec "export PGPASSWORD=\"$POSTGRES_PASSWORD\"; exec pg_restore --host=127.0.0.1 --username=\"$POSTGRES_USER\" --dbname=sshdock_restore --single-transaction --no-password"' \
  <"$DUMP"
```

`createdb` refuses an existing database; `pg_restore --single-transaction` fails atomically on SQL errors. Do not add `--clean` or parallel jobs to this recipe. TOC inspection is not a security audit: restoring trusted SQL/functions can execute database code. [createdb](https://www.postgresql.org/docs/16/app-createdb.html), [pg_restore](https://www.postgresql.org/docs/16/app-pgrestore.html).

Verify application-specific saved rows and a new committed write in `sshdock_restore`, then deliberately point application config at it and redeploy. One-database dumps exclude roles, tablespaces and cluster-wide grants. Provision those separately or review a `pg_dumpall --globals-only` procedure; such scripts may contain password verifiers. [PostgreSQL globals](https://www.postgresql.org/docs/16/app-pg-dumpall.html). Passwords stay out of workstation/SSH command arguments, but trusted container/host administrators can inspect process environments.

## Restore on a cleared or independent host

Test this procedure before relying on it. An untouched live source volume is not evidence of archive recovery.

1. Record the exact SSHDock version, architecture, image pins, app-to-volume mapping, configured paths, desired Git commits and successful attempt IDs. Finish deployments, pause all mutations, stop writers, export data, stop `sshdockd`, and create/inspect the separate host-state archive. Retain verified copies off-host. For PostgreSQL logical recovery, keep its server running for the dump while coordinating application writers.
2. Prepare the compatible destination OS/architecture, Docker/Compose/Caddy, administrator SSH, registry credentials, DNS/firewall and external Caddy credentials. These host/provider credentials and system SSH host keys are not application data or automatically included in the host archive.
3. Install the matching SSHDock version, then stop its daemon. Use the **same configured directory layout** as the source: app rows store absolute repository/worktree paths. The supported host archive copies `DataDir` and the configured Caddy files. Its restore requires the database at `<DataDir>/sshdock.db` and config key at `<DataDir>/config.key`; custom nested database/key paths are not supported. Keep apps, repositories/worktrees and SSH key state under `DataDir`. Paths merely listed in its manifest are not additional copied files.
4. Deliberately restore the host-state archive with `sudo sshdock backup restore <host-archive>`. Re-run the matching bootstrap's ownership normalization if destination service account IDs differ. Keep deployments idle while restoring application data; a backup containing queued work needs separate review before daemon startup.
5. Restore file-volume archives into absent destination volumes and explicitly map them with `external: true` as above. For PostgreSQL logical restore, create a fresh database service/volume and follow the native recipe; do not initialize a supposedly restored database by silently using an old surviving source volume. Never overwrite a populated target to rehearse recovery.
6. Start the daemon, review the restored Git target and Compose mapping, then push the reviewed mapping change or explicitly redeploy current `main`. A new host has no existing containers: `apps start` alone cannot recreate them. Keep stopped writer applications stopped until their data/dependencies are verified.
7. Check diagnostics, operator access, decrypted config, exact new attempt IDs, service health, route state and public HTTPS separately. Verify old application records, new committed writes, and persistence after restart. Update DNS/client configuration deliberately, then resume writers and normal deployment work.

Host-state archive format `sshdock-backup/v1`, `backup create/inspect/restore`, and the explicit `--include-volumes` rejection remain compatible. There is no automatic coordination of multiple volumes/databases, schema reversal, scheduler or storage service.

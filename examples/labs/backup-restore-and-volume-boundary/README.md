# Backup, restore, and volume-boundary feature lab

## Purpose

This lab reuses the stateful [WordPress recipe](../../software/wordpress/README.md) without copying its application, Dockerfile, or Compose file. It creates and inspects a supported SSHDock host-state archive, restores it through the safety flow, proves the restored config key decrypts an app value, and confirms that app metadata, route intent, and named volumes remain observable.

Replace `example.com` below with the SSHDock base domain. This is a host-state restore: run it only on a compatible single-node SSHDock host whose current state you intentionally want to restore. Keep the resulting archive until your normal backup-retention policy allows deleting it.

## Deploy the WordPress recipe

Copy the untouched recipe, create the SSHDock app, and store its required config before the first push. That first successful Git deployment creates the route whose restored intent this lab checks.

```bash
mkdir backup-restore-and-volume-boundary
cd backup-restore-and-volume-boundary
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/software/wordpress
git init -b main
git add .
git commit -m "Deploy WordPress backup and restore lab"
git remote add sshdock git@sshdock.example.com:backup-restore-and-volume-boundary.git
```

Set the required WordPress values and one distinct encrypted value that is not used by the Compose model. Keep `BACKUP_LAB_SECRET` in the current shell; the acceptance script uses it to prove the restored `config.key` decrypts the restored database value.

```bash
WORDPRESS_DB_NAME=wordpress_lab
WORDPRESS_DB_USER=wordpress_lab
WORDPRESS_DB_PASSWORD="$(openssl rand -hex 24)"
WORDPRESS_DB_ROOT_PASSWORD="$(openssl rand -hex 24)"
BACKUP_LAB_SECRET="$(openssl rand -hex 24)"
export BACKUP_LAB_SECRET

sudo sshdock apps create backup-restore-and-volume-boundary
printf '%s\n' "$WORDPRESS_DB_NAME" \
  | ssh sshdock@sshdock.example.com config set backup-restore-and-volume-boundary WORDPRESS_DB_NAME
printf '%s\n' "$WORDPRESS_DB_USER" \
  | ssh sshdock@sshdock.example.com config set backup-restore-and-volume-boundary WORDPRESS_DB_USER
printf '%s\n' "$WORDPRESS_DB_PASSWORD" \
  | ssh sshdock@sshdock.example.com config set backup-restore-and-volume-boundary WORDPRESS_DB_PASSWORD
printf '%s\n' "$WORDPRESS_DB_ROOT_PASSWORD" \
  | ssh sshdock@sshdock.example.com config set backup-restore-and-volume-boundary WORDPRESS_DB_ROOT_PASSWORD
printf '%s\n' "$BACKUP_LAB_SECRET" \
  | ssh sshdock@sshdock.example.com config set backup-restore-and-volume-boundary BACKUP_LAB_SECRET
git push sshdock main
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://backup-restore-and-volume-boundary.example.com
```

Complete WordPress’s first-run setup and create one post before continuing. That data remains in Docker volumes and is deliberately outside this backup archive’s data-copy guarantee.

## Run the executable acceptance overlay

Download the lab script beside the copied WordPress envelope. Set the restricted SSHDock operator target, a normal server-administrator SSH target, the app route, and the exact secret supplied above.

```bash
curl -fsSL https://raw.githubusercontent.com/sshdock/sshdock/main/examples/labs/backup-restore-and-volume-boundary/acceptance.sh \
  -o acceptance.sh

SSHDOCK_TARGET=sshdock@sshdock.example.com \
SSHDOCK_ADMIN_TARGET=admin@example.com \
SSHDOCK_ROUTE_HOST=backup-restore-and-volume-boundary.example.com \
SSHDOCK_BACKUP_LAB_SECRET="$BACKUP_LAB_SECRET" \
bash acceptance.sh
```

Set `SSHDOCK_IDENTITY_FILE=/path/to/key` too when SSH does not already select the operator and administrator key. Set `SSHDOCK_BACKUP_PATH=/root/sshdock-backup-lab.tar.gz` to choose the archive location. The script runs the supported host commands below:

```bash
sudo sshdock backup create --output /root/sshdock-backup-lab.tar.gz
sudo sshdock backup inspect /root/sshdock-backup-lab.tar.gz
sudo systemctl stop sshdockd
sudo sshdock backup restore /root/sshdock-backup-lab.tar.gz
sudo sshdock diagnostics
sudo systemctl start sshdockd
ssh sshdock@sshdock.example.com config get backup-restore-and-volume-boundary BACKUP_LAB_SECRET
sudo docker volume inspect sshdock_backup-restore-and-volume-boundary_wordpress-data
```

The archive contains SSHDock metadata, app repositories and worktrees, generated Caddy route files, key state including `config.key`, and encrypted config material. After restart, the script verifies the restored secret, app health, stored route intent, active Caddy route, and the two WordPress named volumes. Raw output, host names, archive paths, and the value returned by `config get` are private acceptance evidence; keep them under `.local/` rather than in public issue comments.

## Docker-volume boundary

The archive records `docker/volumes.json` as inventory. Docker volume contents are not copied. The script verifies the explicit `--include-volumes` rejection and checks that the existing WordPress and MariaDB volumes are still present after restore; neither check claims an application-consistent data backup or snapshot.

Use WordPress and MariaDB’s own supported export or backup procedures for post content, uploads, and database recovery. SSHDock app removal also preserves named volumes, so remove those separately through normal server administration only when their data is disposable.

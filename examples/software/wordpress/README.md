# WordPress software recipe

## Purpose

This recipe deploys the official WordPress and MariaDB images through SSHDock without custom application code, plugins, themes, seed data, wrapper scripts, or Dockerfiles. It proves first-run setup, persistent content, image-selected upgrades, routine operation, and explicit cleanup.

## Prerequisites

- A working SSHDock server with a base domain configured.
- DNS for `*.example.com` pointing at the server.
- Your public key added to the `sshdock` operator account.
- Local `curl`, `git`, `openssl`, and `tar` commands.

Replace `example.com` below with the server's base domain.

## Topology

The root `compose.yml` runs two services on the private default Compose network: Apache WordPress as `web` and MariaDB as `db`. Only WordPress publishes a port, bound to IPv4 loopback at `127.0.0.1:18200`; MariaDB has no host port. The healthy database gates WordPress startup, both services use `restart: unless-stopped`, and named volumes hold WordPress files and MariaDB data.

The Apache variant is the smallest official WordPress topology that serves HTTP by itself. The smaller FPM Alpine variant requires another FastCGI-capable web service, so it is not the minimal topology for this recipe.

## Pinned versions

- WordPress: `wordpress:7.0.1-php8.3-apache@sha256:d40b86dbdfcfad808a2029acf6543c670c4a61c29f70b9d24605e7d0b31ab83d`
- MariaDB: `mariadb:12.3.2-noble@sha256:628f228f0fd5913a220438693576b29b6fe4dc1fa0a1298c0e98579fae28635f`

These are exact supported version-and-variant tags with multi-platform manifest digests recorded from the Docker Official Images. Recheck upstream support and release notes before changing either pin.

## Deploy

Until a release tag contains this recipe, copy its two public files from `main`:

```bash
mkdir wordpress
cd wordpress
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/software/wordpress
git init -b main
git add .
git commit -m "Deploy WordPress"
git remote add sshdock git@sshdock.example.com:wordpress.git
git push sshdock main
```

The accepted push creates the app but stops before Compose starts because the four required database values are absent. Store them through the restricted SSH surface, confirm output is redacted, redeploy current remote `main`, and attach the conventional route:

```bash
printf '%s' 'wordpress' \
  | ssh sshdock@sshdock.example.com config set wordpress WORDPRESS_DB_NAME
printf '%s' 'wordpress' \
  | ssh sshdock@sshdock.example.com config set wordpress WORDPRESS_DB_USER
openssl rand -base64 32 \
  | ssh sshdock@sshdock.example.com config set wordpress WORDPRESS_DB_PASSWORD
openssl rand -base64 32 \
  | ssh sshdock@sshdock.example.com config set wordpress WORDPRESS_DB_ROOT_PASSWORD
ssh sshdock@sshdock.example.com config list wordpress
sudo sshdock apps redeploy wordpress
sudo sshdock domains attach wordpress web wordpress.example.com --port 18200
```

Open `https://wordpress.example.com`, complete the official first-run form, and publish a representative post before continuing.

## Verify

```bash
curl -I http://wordpress.example.com
curl -fsS https://wordpress.example.com
curl -fsS https://wordpress.example.com/your-post-slug/
sudo sshdock apps health wordpress
sudo sshdock domains check wordpress
sudo sshdock deployments list wordpress
sudo sshdock events list wordpress
```

HTTP redirects to HTTPS, the public route serves the real WordPress site and representative post, and health reports both services with the database private.

## Operate

```bash
sudo sshdock logs wordpress web --tail 100
sudo sshdock logs wordpress db --tail 100
ssh sshdock@sshdock.example.com apps exec wordpress web -- php --version
sudo sshdock apps restart wordpress
sudo sshdock apps health wordpress
curl -fsS --retry 15 --retry-all-errors --retry-delay 2 https://wordpress.example.com/your-post-slug/
```

## Upgrade

Choose supported exact WordPress and MariaDB version-and-variant tags, resolve their current multi-platform digests, review both upstream upgrade guides, and update `compose.yml` plus the recorded pins above. Commit the pin change so Git selects the new deployment:

```bash
WORDPRESS_DB_NAME=wordpress \
WORDPRESS_DB_USER=wordpress \
WORDPRESS_DB_PASSWORD=local-only-password \
WORDPRESS_DB_ROOT_PASSWORD=local-only-root-password \
  docker compose pull
git add compose.yml README.md
git commit -m "Upgrade WordPress recipe"
git push sshdock main
```

After the deployment succeeds, verify the representative post and route again. The persistent WordPress volume follows the official image's self-managed core-update model; changing the image pin updates the container runtime and bundled source, but does not replace already-initialized site files wholesale.

## Cleanup

Ordinary SSHDock removal deletes app-owned containers and state while preserving Docker volumes:

```bash
sudo sshdock apps remove wordpress --force
sudo docker volume ls --format '{{.Name}}' | grep '^sshdock_wordpress_'
```

Only when you intentionally want to destroy the site files, uploads, themes, plugins, and database, remove both volumes after removing the app:

```bash
sudo docker volume rm sshdock_wordpress_wordpress-data sshdock_wordpress_mariadb-data
```

## Persistence

`wordpress-data` stores initialized WordPress files, uploads, themes, and plugins at `/var/www/html`. `mariadb-data` stores the database at `/var/lib/mysql`. Restart, redeploy, exact-image upgrades, and ordinary app removal preserve both named volumes; SSHDock does not back up their contents.

## Limitations

This recipe proves the upstream first-run UI, a real post, HTTPS routing, database readiness, logs, restart, redeploy, exact-image upgrade, persistence, and cleanup. It does not provide managed WordPress updates, application-consistent backups, object storage, SMTP, cache, staging, high availability, or zero-downtime deployment.

## Security boundaries

MariaDB is reachable only on the private Compose network, the WordPress HTTP port binds only to host loopback, and Caddy remains the public TLS entry point. Database values live in SSHDock's encrypted config rather than Git and stay redacted on normal operator surfaces. Operators remain responsible for unique administrator credentials, WordPress and plugin security updates, backups, SMTP, least-privilege plugins, and host security; SSHDock's trusted-owner model does not sandbox malicious Compose workloads or WordPress extensions.

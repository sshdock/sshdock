# WordPress Lite

WordPress + MariaDB Compose example with named volumes. SSHDock should infer `web:18081` from `127.0.0.1:18081:80`.

Credentials in `compose.yml` are demo-only. For the fuller walkthrough, see [`../../docs/EXAMPLES.md`](../../docs/EXAMPLES.md).

## Deploy

Replace `example.com` with your SSHDock base domain.

```bash
mkdir wordpress-lite
cd wordpress-lite
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/wordpress-lite/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/wordpress-lite/README.md
git init -b main
git add .
git commit -m "Deploy WordPress lite"
git remote add sshdock git@sshdock.example.com:wordpress-lite.git
git push sshdock main
```

## Verify

```bash
sudo sshdock apps list
sudo sshdock domains list wordpress-lite
sudo sshdock events list wordpress-lite
sudo sshdock logs wordpress-lite web
sudo sshdock logs wordpress-lite db
ssh -T sshdock@sshdock.example.com
```

```bash
curl -I http://wordpress-lite.example.com
curl -fsSI https://wordpress-lite.example.com
```

Expected: `wordpress-lite` is healthy, `wordpress-lite.example.com` routes to `web:18081`, HTTP redirects to HTTPS, and HTTPS reaches the WordPress installer.

## Clean Up

```bash
sudo sshdock apps remove wordpress-lite --force
sudo sshdock apps list
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_wordpress-lite-' || true
sudo docker volume ls --format '{{.Name}}' | grep '^sshdock_wordpress-lite_'
```

```bash
cd ..
rm -rf wordpress-lite
```

Expected: `wordpress-lite` no longer appears, no `sshdock_wordpress-lite-*` containers remain, and named volumes are preserved.

To erase the demo data too:

```bash
sudo docker volume rm sshdock_wordpress-lite_wordpress-data sshdock_wordpress-lite_mariadb-data
```

Only run the volume removal command when you intentionally want to erase the demo uploads, plugins, themes, and database. For real use, replace the demo passwords and plan backups, updates, SMTP, cache, and hardening yourself.

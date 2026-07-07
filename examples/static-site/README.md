# Static Site

Minimal nginx example. SSHDock should infer `web:18080` from `127.0.0.1:18080:80`.

For the fuller walkthrough, see [`../../docs/EXAMPLES.md`](../../docs/EXAMPLES.md).

## Deploy

Replace `example.com` with your SSHDock base domain.

```bash
mkdir static-site
cd static-site
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site/README.md
mkdir public
curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site/public/index.html
git init -b main
git add .
git commit -m "Deploy static site"
git remote add sshdock git@sshdock.example.com:static-site.git
git push sshdock main
```

## Verify

```bash
sudo sshdock apps list
sudo sshdock domains list static-site
sudo sshdock events list static-site
sudo sshdock logs static-site web
ssh -T dashboard@sshdock.example.com
```

```bash
curl -I http://static-site.example.com
curl -fsS https://static-site.example.com
```

Expected: `static-site` is healthy, `static-site.example.com` routes to `web:18080`, HTTP redirects to HTTPS, and HTTPS contains `SSHDock static site OK`.

## Clean Up

```bash
sudo sshdock apps remove static-site --force
sudo sshdock apps list
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_static-site-' || true
```

```bash
cd ..
rm -rf static-site
```

Expected: `static-site` no longer appears, no `sshdock_static-site-*` containers remain. No Docker volumes need to be removed for this static example.

# Rollback Lab Example

This example proves the operator story for a bad deploy and rollback. It starts as a valid static site, then asks you to commit one intentionally broken Compose change.

Deploy the good release:

```bash
mkdir rollback-lab
cd rollback-lab
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/rollback-lab/compose.yml
curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/rollback-lab/README.md
mkdir public
curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/main/examples/rollback-lab/public/index.html
git init -b main
git add .
git commit -m "Deploy rollback lab"
git remote add sshdock git@sshdock.example.com:rollback-lab.git
git push sshdock main
curl -fsS https://rollback-lab.example.com
```

Create a bad deploy:

```bash
perl -0pi -e 's/image: nginx:alpine/image: nginx:no-such-tag-for-rollback-lab/' compose.yml
git add compose.yml
git commit -m "Break rollback lab image"
git push sshdock main
```

Expected bad-deploy evidence:

- The Git push may complete because SSHDock deploys from a post-receive hook.
- The deploy fails and records `deploy.failed`.
- `sudo sshdock releases list rollback-lab` shows the original successful release and the failed release.
- `sudo sshdock events list rollback-lab` includes `deploy.failed`.
- The previous good app remains recoverable through rollback.

Rollback:

```bash
sudo sshdock releases list rollback-lab
sudo sshdock apps rollback rollback-lab <successful-release-id>
curl -fsS https://rollback-lab.example.com
```

Expected rollback evidence:

- HTTPS returns `SSHDock rollback lab OK`.
- `events list rollback-lab` includes `rollback.triggered` and `rollback.succeeded`.
- The dashboard shows release and deployment history.

## Clean Up

```bash
sudo sshdock apps remove rollback-lab --force
sudo docker ps -a --format '{{.Names}}' | grep '^sshdock_rollback-lab-' || true
```

No Docker volumes need to be removed.

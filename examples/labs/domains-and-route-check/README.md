# Domains and route check feature lab

## Purpose

This lab reuses the WordPress software recipe to prove automatic routing after a successful Compose deployment, manual route attach and detach, active Caddy checks from fresh processes, safe unavailable-state reporting, and a valid Caddy configuration after the final route is removed. It does not copy WordPress application files or its root Compose file: the database remains private while only the recipe's `web` service can receive a public route.

## Prerequisites

- A working SSHDock server with a base domain and wildcard DNS configured.
- Your public key added to the `sshdock` operator account and normal administrator access to the host.
- Local `curl`, `git`, `openssl`, and `tar` commands.

Use two hostnames under that base domain. In the examples, the automatic route is `domains-and-route-check.example.com` and the manual route is `manual-domains-and-route-check.example.com`. Replace `example.com` with the server's base domain.

## Deploy

Copy the untouched WordPress deployment envelope from `main` until a release tag contains this lab:

```bash
mkdir domains-and-route-check
cd domains-and-route-check
curl -fsSL https://github.com/sshdock/sshdock/archive/refs/heads/main.tar.gz \
  | tar -xz --strip-components=4 sshdock-main/examples/software/wordpress
git init -b main
git add .
git commit -m "Deploy WordPress domains and route check lab"
git remote add sshdock git@sshdock.example.com:domains-and-route-check.git
```

Create the receive repository without deploying it, then store the database values before the first Git push. `apps create` does not run Compose or create a route; this makes the first deployment the successful one that creates the conventional automatic route:

```bash
sudo sshdock apps create domains-and-route-check
printf '%s' 'wordpress' \
  | ssh sshdock@sshdock.example.com config set domains-and-route-check WORDPRESS_DB_NAME
printf '%s' 'wordpress' \
  | ssh sshdock@sshdock.example.com config set domains-and-route-check WORDPRESS_DB_USER
openssl rand -base64 32 \
  | ssh sshdock@sshdock.example.com config set domains-and-route-check WORDPRESS_DB_PASSWORD
openssl rand -base64 32 \
  | ssh sshdock@sshdock.example.com config set domains-and-route-check WORDPRESS_DB_ROOT_PASSWORD
ssh sshdock@sshdock.example.com config list domains-and-route-check
git push sshdock main
ssh sshdock@sshdock.example.com domains list domains-and-route-check
ssh sshdock@sshdock.example.com domains check domains-and-route-check
```

The `domains list` row must name `web` on port `18200`; it must not name the private `db` service. Complete WordPress's normal first-run form at the automatic HTTPS route before running the full acceptance script.

## Verify

Run the executable overlay with the exact automatic and manual hostnames:

```bash
SSHDOCK_TARGET=sshdock@sshdock.example.com \
SSHDOCK_ADMIN_TARGET=admin@example.com \
SSHDOCK_AUTO_ROUTE_HOST=domains-and-route-check.example.com \
SSHDOCK_MANUAL_ROUTE_HOST=manual-domains-and-route-check.example.com \
bash acceptance.sh
```

Set `SSHDOCK_IDENTITY_FILE=/path/to/key` too when SSH does not already select the operator and administrator key. The script opens fresh SSHDock processes for every check. It proves that the automatic route is active, adds and checks a manual route, temporarily points only the check process at an unavailable local Caddy admin endpoint to show actionable `unavailable` output, removes the manual route, removes the final automatic route, and validates the host Caddy configuration. It then removes the app while preserving the two named volumes. For an isolated repeat run, set `SSHDOCK_APP` and derive both route hostnames from that app name.

To perform the manual lifecycle without the script:

```bash
sudo sshdock domains attach domains-and-route-check web manual-domains-and-route-check.example.com --port 18200
ssh sshdock@sshdock.example.com domains check domains-and-route-check
sudo sshdock domains detach domains-and-route-check manual-domains-and-route-check.example.com
sudo sshdock domains detach domains-and-route-check domains-and-route-check.example.com
ssh sshdock@sshdock.example.com domains check domains-and-route-check
```

## Cleanup

The script removes the app after the final route check. To stop before that final step, remove it explicitly:

```bash
ssh sshdock@sshdock.example.com apps remove domains-and-route-check --force
sudo docker volume ls --format '{{.Name}}' | grep '^sshdock_domains-and-route-check_'
```

Ordinary removal preserves WordPress and MariaDB volumes. Remove those volumes only through normal server administration when their content is intentionally disposable.

## Limitations

This lab proves HTTP/HTTPS routing, route-state inspection, private-service non-exposure, and Caddy configuration validity. It does not provision DNS, alter firewalls, expose MariaDB, or turn SSHDock into a TCP router, certificate manager, or managed database service.

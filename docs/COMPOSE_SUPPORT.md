# SSHDock Compose Support

SSHDock v0 deploys Docker Compose apps on one node. Docker Compose is the schema authority for application fields: SSHDock passes standard services, images, builds, commands, ports, networks, volumes, resources, health checks, configs, secrets, labels, profiles, and other Compose fields to `docker compose config` instead of maintaining a second field allowlist.

## Root File Detection

Commit exactly one of these files at the repository root:

- `compose.yaml`
- `compose.yml`
- `docker-compose.yaml`
- `docker-compose.yml`

If none are present, deployment fails and lists all four expected names. If more than one is present, deployment fails and lists the conflicting files; SSHDock never guesses which definition to use.

Custom Compose file flags are not supported.

## External File Boundary

The selected root file must contain the complete Compose application definition.

SSHDock rejects:

- top-level `include`
- service `extends.file` references to another Compose file

The error names the external file and asks you to keep the application in the selected root file. SSHDock uses Compose interpolation semantics and the effective process, app-config, and project `.env` values when checking `extends.file`. Same-file Compose behavior remains Docker Compose behavior, including YAML anchors, extension fields such as `x-service`, `extends` with only a same-file `service` reference, and `extends.file` that resolves to the selected root file itself.

## Compose Project Isolation

Every Compose operation uses one SSHDock-controlled project name derived from the validated app name:

```text
sshdock_<app-name>
```

This project name takes precedence over a top-level Compose `name`, keeping ordinary containers, networks, and named volumes isolated between apps. Explicit global resource names, external resources, bind mounts, host networking, and similar host-coupled settings remain operator-owned Compose behavior.

## Native Deploy And Health Semantics

SSHDock asks Docker Compose to execute the application in this order:

```text
docker compose config --format json
docker compose pull --ignore-buildable
docker compose build
docker compose up -d --wait --wait-timeout 120
```

The final operation also has a host-side two-minute deadline. Docker Compose decides whether each service is ready: an effective container health check must become healthy, while a service without one must remain running. An unhealthy service, an immediate exit, or an expired wait records a failed deployment at the `start and wait for services` stage.

SSHDock does not generate release Compose overrides, tag build images with commit or `latest` release tags, prune release images, or automatically roll back a failed deployment. BuildKit and Docker own build cache and image garbage collection. A failed replacement may have changed containers already, and any existing route remains pointed at the same published host port. This is readiness observation, not blue-green or zero-downtime traffic switching.

## Trusted Owner Warnings

SSHDock reports warnings without rejecting Compose models that:

- publish a port on all interfaces instead of IPv4 loopback
- enable privileged mode or host networking
- mount the Docker socket or another host bind path
- use an explicit global volume name or an external volume

These warnings make host coupling visible; they do not sandbox the workload. A trusted Compose push can have host-level impact, so deploy access belongs only to trusted server owners.

## Config And Secrets

Apps may commit `.sshdock.yml` to declare required SSHDock config keys. SSHDock resolves stored values before Compose starts and passes them only through the Compose process environment. It does not auto-inject every stored value into every service.

Compose `configs` and `secrets` fields are passed to Docker Compose normally. The external-file boundary above applies to Compose definitions loaded through `include` or `extends.file`, not ordinary files referenced by application fields.

See [`CLI_COMMANDS.md`](CLI_COMMANDS.md) for `config set`, `config import`, `config list`, and `config get`.

## Routing Boundary

After a deployment succeeds, automatic route creation consumes the effective model returned by `docker compose config`. It prefers a `web` service with exactly one published TCP port, then a single `127.0.0.1`-bound candidate, then a unique one-port service. Automatic routing accepts host bindings that the current Caddy upstream can reach: an unset host IP, `0.0.0.0`, or `127.0.0.1`. An IPv6-only or specific-host binding deploys without an automatic route and receives manual attach guidance, as do worker-only, private, missing-port, and ambiguous apps.

Only an app without an existing domain receives an inferred initial route. A failed first deployment remains unrouted, and a later failed replacement does not rewrite an existing route.

The most reliable routed shape is one public web service with one loopback-published TCP port:

```yaml
services:
  web:
    image: nginx:alpine
    ports:
      - "127.0.0.1:3000:80"
```

When automatic inference is not enough, attach a route explicitly:

```bash
sudo sshdock domains attach my-app web app.example.com --port 3000
```

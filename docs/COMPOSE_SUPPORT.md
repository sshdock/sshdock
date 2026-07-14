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

## Config And Secrets

Apps may commit `.sshdock.yml` to declare required SSHDock config keys. SSHDock resolves stored values before Compose starts and passes them only through the Compose process environment. It does not auto-inject every stored value into every service.

Compose `configs` and `secrets` fields are passed to Docker Compose normally. The external-file boundary above applies to Compose definitions loaded through `include` or `extends.file`, not ordinary files referenced by application fields.

See [`CLI_COMMANDS.md`](CLI_COMMANDS.md) for `config set`, `config import`, `config list`, and `config get`.

## Routing Boundary

Automatic route creation depends on a safely inferred host-published TCP port. Worker-only apps and private dependency services can deploy without a public route.

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

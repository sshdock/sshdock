# SSHDock Compose Support

SSHDock v0 deploys Docker Compose apps on one node. The supported contract is intentionally small: enough for solo-developer web apps, workers, private dependencies, config interpolation, health checks, and named volumes.

This is not a promise of full Docker Compose compatibility. Unsupported fields fail during deploy validation before SSHDock starts Compose.

## File Detection

SSHDock looks for one Compose file at the repository root:

- `compose.yml`
- `docker-compose.yml`

## Supported Top-Level Fields

- `services`
- `volumes`

`services` must be a mapping and must define at least one service.

## Supported Service Fields

- `image`
- `build`
- `environment`
- `env_file`
- `depends_on`
- `volumes`
- `ports`
- `expose`
- `healthcheck`
- `restart`

Services may use published ports for SSHDock route inference. The most reliable routed shape is one public web service with one loopback-published TCP port:

```yaml
services:
  web:
    image: nginx:alpine
    ports:
      - "127.0.0.1:3000:80"
```

## Known Unsupported Fields

Unsupported top-level fields include:

- `networks`
- `secrets`
- `configs`

Unsupported service fields include:

- `command`
- `entrypoint`
- `container_name`
- `labels`
- `deploy`
- `profiles`

If your app needs an unsupported field, keep the workaround inside the image or Dockerfile when possible. For example, prefer a small wrapper script baked into the image instead of a Compose `command` override.

## Config And Secrets

Apps may commit `.sshdock.yml` to declare required config keys. SSHDock resolves stored config values before Compose starts and passes them only through the Compose process environment. It does not auto-inject every stored value into every service.

See [`CLI_COMMANDS.md`](CLI_COMMANDS.md) for `config set`, `config import`, `config list`, and `config get`.

## Routing Boundary

Automatic route creation depends on a safely inferred host-published TCP port. Worker-only apps and private dependency services can deploy without a public route.

When automatic inference is not enough, attach a route explicitly:

```bash
sudo sshdock domains attach my-app web app.example.com --port 3000
```

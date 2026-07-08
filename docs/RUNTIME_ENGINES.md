# Runtime Engine Boundary

SSHDock is Compose-first at the user boundary.

Users should keep deploying the same way:

```text
git push -> SSHDock release -> SSH dashboard operations
```

For the current release line, Docker Compose is the only implemented runtime engine. Apps use `compose.yml` or `docker-compose.yml`, services are started through Docker Compose, Caddy routes to loopback-published service ports, and SQLite stores SSHDock metadata.

## Why Keep An Engine Boundary

The current Compose runner is enough for the v0 happy path:

- one VPS
- one Compose app per repo
- build or pull images
- start services
- attach routes
- inspect logs and status
- restart, redeploy, and rollback

More advanced stories need stronger runtime primitives than plain Compose provides comfortably:

- health-gated rollout and cutover
- background jobs and scheduled jobs
- secret/config projection
- persistent volume lifecycle
- service discovery
- internal ingress and routing models
- controlled rollout and rollback of multiple services

The engine boundary exists so SSHDock can explore a stronger runtime later without changing the main user workflow.

## k3s Direction, Not Promise

k3s is a plausible future advanced runtime engine because it packages Kubernetes primitives into a single-node-friendly form. It could map common SSHDock concepts to stronger primitives:

| SSHDock / Compose concept | Possible k3s mapping |
| --- | --- |
| app release | rendered workload bundle |
| web service | Deployment plus Service |
| worker service | Deployment with no public Ingress |
| one-off task | Job |
| scheduled task | CronJob |
| environment config | ConfigMap or Secret |
| named volume | PersistentVolumeClaim |
| service healthcheck | readiness and liveness probes |
| public route | Ingress |
| rollback | previous rendered workload bundle |

This is not a product promise. SSHDock may still pivot away from k3s if dogfood evidence shows another engine, a narrower Compose extension, or a different deployment model fits better.

## Current Scope

M22 does not implement a k3s engine.

M22 does not change runtime code, CLI commands, deploy behavior, install behavior, or the supported v0 runtime. It only documents common user stories, adds example coverage, and records the runtime boundary that future engine work should preserve.

## Non-Goals

The runtime boundary does not promise:

- high availability
- multi-node scheduling
- managed Kubernetes operations
- a public Kubernetes API surface
- CRDs or operators
- service mesh
- autoscaling
- hosted cloud features
- full Docker Compose compatibility

The product should still feel like an SSH-native PaaS for solo developers, not a Kubernetes distribution with a thin wrapper.

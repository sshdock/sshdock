# One routing mutation owner

Status: accepted

## Decision

`internal/router.Service` owns domain intent, target/address validation, domain and router events, complete route rebuilding, Caddy application, and apply-failure recording and cleanup. Caddy remains an adapter that applies a complete snapshot and reads active routes.

CLI attach/detach, deployment initial routing, and post-removal reconciliation use this module. Deployment still infers the target service and host port from Compose. The routing owner checks whether the app already has a route while holding the same lock as the potential initial attachment.

A cross-process route lock covers intent mutation and snapshot reading through Caddy application and failure cleanup. Successful reconciliation clears failures only after the full current intent has been applied. Removal may reconcile after its app record is gone; domain persistence atomically rejects an app that has been deleted.

Ports are `1..65535`. Public endpoint ownership uses canonical hostname plus effective listen port; HTTP/HTTPS intent remains explicit. Different apps cannot claim the same endpoint. New domain IDs preserve distinct addresses instead of replacing punctuation with underscores.

## Consequences

There is one mutation flow and one test surface for routing. Manual apply failures return an error while retaining desired intent for retry. Initial automatic apply failures retain successful container deployment and record recoverable routing failure state. Operation audit or failure-cleanup errors remain visible and do not masquerade as Caddy reload failure.

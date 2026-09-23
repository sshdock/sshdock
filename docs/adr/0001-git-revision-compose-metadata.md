# Git revision metadata for Compose

Status: accepted

## Decision

SSHDock provides `SSHDOCK_GIT_SHA` in the Compose process environment. During a deployment it is the full accepted commit recorded in that deployment attempt. A redeploy resolves remote `main`, checks it out, and uses that attempt's commit. No persistent app-config mutation or separate deployment protocol selects an image.

Operational commands resolve the revision of the Compose worktree through the managed bare repository's detached `HEAD`. They do not substitute a newer queued `main`, or the latest successful release after a failed checkout/deployment. SSHDock's existing checkout uses a full commit ID. Before the first detached checkout, the metadata is empty; required Compose interpolation fails rather than trusting a process or `.env` fallback. Unreadable or malformed metadata reports recovery guidance.

This metadata overrides inherited process and project `.env` values. The existing `SSHDOCK_` app-config reservation remains in force. The value is public metadata, separate from encrypted config values and secret redaction. Containers receive it only when the committed Compose model references it.

## External build contract

An external CI builds and publishes an OCI image tagged with the full source commit, then pushes that exact commit to SSHDock `main`. An image-only Compose service references `image: registry.example/app:${SSHDOCK_GIT_SHA:?deploy a commit}`. Compose retains its normal pull/build/up behavior; `build` has no application image to build when the model has no `build:` services.

This deterministically selects a tag, not immutable registry content. The registry owner must prevent rewriting commit tags and retain images needed for redeploy or Git-selected recovery. Missing tags, credentials or target-platform images fail through the existing observable deployment path.

## Consequences

Local Build and External Build share one Git/Compose contract, deployment queue, health checks and history. There are no generated Compose overrides, hidden image mappings, managed registry/CI features, or schema changes. Existing per-app busy-push rejection and Git fast-forward rules remain unchanged.

Git's [repository layout](https://git-scm.com/docs/gitrepository-layout#Documentation/gitrepository-layout.txt-HEAD) defines detached `HEAD`; Docker's [interpolation precedence](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/) gives the process environment priority over `.env`.

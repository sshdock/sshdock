# One deployment executor

Status: accepted

## Decision

`internal/deployment` owns execution of an admitted deployment attempt: app lookup, checkout of the attempt's fixed commit, Compose detection and validation, config and secret redaction, release bookkeeping, durable output, warnings, terminal state, events, and initial route selection.

Git receive reserves an attempt before the ref update and queues it only after acceptance. The daemon claims that attempt and calls `Execute`. Redeploy resolves remote `main` and atomically queues a pending attempt, durable log, and queue event under the same per-app exclusion. The daemon claims it through the existing queue; the requesting client waits for that exact attempt and may disconnect without canceling it. CLI and TUI callers request redeploy without arranging runtime steps or interpreting a Compose result.

Both producers use `deploy.*` lifecycle events; the attempt's `trigger` records its origin. A release identifies an app commit and keeps its past success across failed retries. Each retry records a separate attempt.

## Consequences

There is one checkout-to-outcome implementation and one test surface for deployment behavior. Git admission remains separate from execution because it must precede ref acceptance. The daemon owns execution and interrupted-attempt finalization for both triggers. A terminal success is visible only after initial routing and outcome persistence. Redeploy still waits for completion; this decision reuses the existing queue and does not change Git's fast-forward and lease rules. Revision metadata follows ADR 0001.

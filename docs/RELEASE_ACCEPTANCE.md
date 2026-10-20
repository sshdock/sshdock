# v1.0.0 release validation

Functional checks passed within the environment and limits below. Bounded network observations retain their nonzero outcomes. See [issue #49](https://github.com/sshdock/sshdock/issues/49).

## Source boundary

Acceptance used public [v1.0.0-rc.3](https://github.com/sshdock/sshdock/releases/tag/v1.0.0-rc.3) assets/bootstrap from [this runtime revision](https://github.com/sshdock/sshdock/commit/0cf5a3ea5a4aee59857a5308c7046e50f1c74c39). Checksums, installed provenance, CI/security and both architecture builds passed. Runtime Go code, bootstrap, dependencies and release workflows remain unchanged. Later verified deltas are [Next.js 16.4.2/production dependencies](https://github.com/sshdock/sshdock/commit/9430b936a0d4800f94bc95df3ba3b9747208a838), [its acceptance assertion](https://github.com/sshdock/sshdock/commit/2f8ebc5822a69c03af29bcbaebf363213289af0f) and [n8n readiness](https://github.com/sshdock/sshdock/commit/1a8b3d01042aecf71e4b9d830ca546ed69fa1b73).

The release pipeline runs repository CI and reachable-vulnerability checks before building versioned archives/checksums. See [v1.0.0 release notes](https://github.com/sshdock/sshdock/releases/tag/v1.0.0) and issue #49 for final source checks and post-publication stable-asset, provenance and matching-installer verification.

## Completed checks

| Area | Result |
| --- | --- |
| Public install/repeat install | PASS: matching downloads and preserved state, keys/auth, routes, containers and history. |
| Local/External Build | PASS: restricted Git, VPS build or commit-tagged hosted-CI image pull without application build, runtime identity and public/custom HTTPS. |
| Recovery/routing | PASS: durable queue/client detach/daemon restart, failed-image/Git recovery, reboot without replay, route/listener boundaries and administrator Caddy-site preservation. |
| Frameworks | PASS: Next.js, NestJS, Laravel, Gin, Phoenix; real LiveView WebSocket reconnect and persisted browser records. |
| Databases | PASS: loopback/tunneled PostgreSQL SQL; shared PostgreSQL role/database/network isolation and persistence. |
| Software | PASS: WordPress, Gitea, n8n, Memos, Planka setup/state, image upgrades, lifecycle, persistence, cleanup and final audit, with limitations below. |
| Seven feature labs | PASS: External Build, config, Git recovery, health/history/logs, restricted SSH, backup/restore and domains. |
| Installed v0.3.1 upgrade | PASS: public rc.3 upgrade, legacy-key/account/wrapper migration, encrypted config/key/history/container preservation without replay, same-SHA redeploy and distinct-commit Git push. |

The installed upgrade used matching public scripts/assets, normal paths and `SSHDOCK_BOOTSTRAP_INSTALL_DEPS=0` after fresh dependency installation. Old/new SSH used localhost; TLS retained hostname/SNI over loopback. Workstation ingress was separately verified. Five archives were downloaded, checksum-matched and payload-read; cleanup/protected boundaries passed.

Old setup initially returned `SQLITE_BUSY`, cause unproven. Seeding with the daemon paused preceded one stable startup/deployment; automatic first-start recovery is not established.

Backup/restore recovered config/key and route intent; the WordPress post remained in live volumes. Golden state changed only generated-fragment ownership, preserving bytes/mode. `phx_new` 1.8.9 resolved Phoenix 1.8.16 / LiveView 1.2.13.

## Observation and limits

The quiet observation completed **3600.01s**, **147 rounds / 294 requests per channel**. Python DNS-dependent HTTPS had **1 TLS-handshake timeout**; verified curl DNS and fixed-IP channels each had **0 failures**. Its **exit 1** remains retained. Boot, services, containers, restart/OOM counts and active Caddy configuration stayed unchanged, with no invariant errors, Caddy mutations or material clock-suspend gap.

A separate **1180.361s / 495-request** diagnostic retained **exit 1**: Python DNS **0/142 failures**, fixed-IP Python **1/142**, curl HTTP/1.1 DNS **0/141**, external control **0/70**. Its server capture contained **9740 records / 0 socket drops**. The failed 13-packet flow completed TCP setup without captured payload/advancing server ACK. This reproduces failure without target DNS selection, not its cause.

One reversible **9-request MSS experiment** succeeded throughout. Advertised MSS **8960→1200→8960** changed TLS time **1.19–1.36s→about 0.070s→1.20–1.21s**, with the pre-response receive-sequence gap disappearing and returning. This supports MSS/segmentation sensitivity on this client/path, not an exact MTU/hop or an explanation/fix for any 20-second failure. Rules and protected state were restored; the cleanup timer/service was inactive. No permanent tuning or product fix resulted.

These apps were freshly recreated; the new baseline does not preserve earlier identities/history. Sequential client comparisons alone do not establish causation.

The maintenance observation lasted **3604.221s / 288 requests per channel**: Python DNS HTTPS had **3 failures** (two TLS timeouts, one closed connection), verified fixed-IP curl **0**. Metadata stayed unchanged. All three observation windows retain nonzero outcomes, without an SLA/load guarantee or historical causal attribution.

Gitea public SSH **18222 timed out**. Its push and **four independent clones** used an administrator tunnel. Direct public ingress remains unverified.

n8n exceeded native **120s** waits on baseline/current images; each failure received **one explicit same-SHA redeploy after readiness**. Owner/workflow and a new execution survived upgrade to 2.30.5. Baseline readiness observations were **427.8s/387.97s**; the former's migrations finished around **70s**. Current-image **494.8s** includes an observation gap, an upper bound. No sole cause/performance guarantee is assigned. Wait for readiness after start/restart; diagnose failed recovery rather than looping deployments.

Live evidence covers one software-reset **Ubuntu 24.04/amd64** VPS, not an OS reimage. Arm64 was cross-built; Debian/arm64 were not tested live. No local Docker was used. SSHDock remains trusted-operator, single-node Compose, without automatic rollback, workload isolation or zero-downtime cutover.

Config is encrypted on disk; **backup tar archives are not encrypted** and contain the decryption key. Protect them as credentials. Volumes are inventoried, not copied: retained live data does not prove archive recovery of application contents. Host maintenance/application backups remain operator responsibilities; upstream compatibility and security checks are point-in-time evidence.

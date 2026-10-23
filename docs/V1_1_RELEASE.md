# OSS v1.1.0 release notes

SSHDock remains an Apache-2.0, single-VPS Git/SSH/Compose deployment tool. Its manual operations run without an SSHDock Cloud account, login or subscription.

## Included capabilities

- [Structured inspection](JSON_OUTPUT.md): schema-versioned JSON for supported reads and exact deployment detail, separating desired main, latest attempt, last success, observed services and routes. Human-readable output remains the default.
- [Read-only preflight](PREFLIGHT.md): local project and existing-app checks with explicit blockers and unknowns. It does not pull/build images or prove application readiness/public availability.
- [Manual Git-selected recovery](RECOVERY.md): verify a compatible local target, obtain owner approval and use the full expected-main lease; verify the new exact attempt. No automatic rollback or database migration reversal is included.
- [Manual application-data backups](APPLICATION_DATA_BACKUPS.md): stopped-writer ordinary file-volume archives and PostgreSQL-native logical dump/restore, separate from SSHDock host-state backups. Fresh-target recovery was tested after source data removal.
- [Portable coding-agent operations](AGENT_OPERATIONS.md): one reviewed skill for project-local Codex and Claude Code installation, using shipped Git, SSH and CLI commands. A live native Codex journey verified deploy, failure diagnosis and explicitly leased recovery.

## Installation and distribution

Use the matching v1.1.0 bootstrap and assets from the [installation guide](INSTALL.md#published-release-v110). Linux amd64 and arm64 archives each contain the CLI, daemon, Apache license and third-party notices, with a matching checksum sidecar. The skill and its standalone license ship in the versioned source archive under `.agents/skills/sshdock-operations`; follow the documented copy procedure rather than looking for a hosted agent service.

The public v1.1.0-rc.1 gate verified anonymous assets, matching bootstrap/source, checksums, licenses/notices and binary build provenance for both architectures. Actual cleared-runtime RC installation/repeat and populated public-v1-to-RC upgrade/repeat passed on Ubuntu amd64, preserving keys/config, data, running services, routes, history and explicitly resupplied service settings. Cross-compilation and archive checks establish build coverage, not live arm64 runtime acceptance. See the [acceptance matrix](V1_1_ACCEPTANCE.md) for exact revisions, reused backup proof and retained failures; stable-specific publication checks are recorded in the final GitHub release notes.

## Upgrade from v1.0.0

1. Record the current install layout, reviewed nondefault bootstrap settings, service drop-ins and external Caddy configuration. Pause pushes/operator mutations and wait for deployments to finish. Take the documented host-state backup with the daemon stopped, plus tested application-native or stopped-writer data backups under their own consistency windows. Copy backups off-host and protect them as credentials. v1.0.0 does not provide the new `backup volume` commands; host-state archives do not copy application data.
2. Run the target release's matching bootstrap/assets, explicitly resupplying the recorded settings. The installer regenerates the daemon unit and Git/operator wrappers from its invocation environment; it does not import old unit settings. Preserve external Caddy credentials/configuration and service drop-ins separately. Caddy admin addresses use bare `host:port`, not URL syntax. See [upgrade notes](INSTALL.md#upgrade-notes).
3. Verify both binary versions, diagnostics and a fresh operator SSH connection. Check desired/latest/last-success identities, services, active routes, public HTTPS and actual saved application data separately. A successful install or deployment alone does not establish these outcomes. Completed deployment history should remain intact without replay; explicitly recover an interrupted failed attempt rather than assuming it reran.

Existing app Git remotes, Compose authority, encrypted config and default human-readable commands remain compatible. JSON consumers must check schema version `"1"`, process exit, `error` and result state; query success is not application health. Review and deliberately update installed skill copies when changing server/client versions.

## Support and operational limits

Live evidence covers one software/data-reset Ubuntu 24.04 amd64 VPS with retained OS/runtime prerequisites, not an OS reimage or independent second VPS. Debian and arm64 live checks and a live Claude journey remain untested. The historical complete framework/software/browser/long-observation matrix was not rerun for v1.1; its results remain tied to the earlier release.

SSHDock assumes a trusted owner with node-level Docker privileges. It does not provide workload isolation, multi-node failover, zero-downtime traffic switching, automatic rollback, scheduled backups or an availability SLA. Failed Compose replacement can change serving state even when history contains an earlier success.

Volume archives support only the documented ordinary-file/directory and numeric-owner/basic-mode boundary; database consistency needs application-native backups. Archives are unencrypted and may contain secrets; host-state archives include the config decryption key. Restore into fresh targets and verify old data, new writes and restart persistence before relying on a backup. Vulnerability scans and upstream compatibility checks are point-in-time evidence, not an ongoing guarantee.

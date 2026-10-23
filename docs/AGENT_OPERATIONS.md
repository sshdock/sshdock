# Coding-agent operations

SSHDock ships one portable [sshdock-operations skill](../.agents/skills/sshdock-operations/SKILL.md) for trusted-owner operations. Codex and Claude Code use the same instructions and the same OSS CLI, Git and SSH commands. There is no embedded agent, hosted control plane or separate deploy API.

The skill targets **v1.1.0**: v1.0.0 does not have its JSON, preflight or volume-data commands. Use a reviewed SSHDock checkout matching the server/client release, and read that version's docs. The skill links to the matching `v1.1.0` documentation; use the skill and docs from the same reviewed tag when upgrading.

## Install into an application repository

Review the skill before giving an agent credentials. Copy the whole directory from a trusted checkout into the application repository. The examples refuse an existing destination; review updates deliberately rather than overwriting another skill with the same name. They do not modify personal/global agent settings.

For **Codex**, use the repository's `.agents/skills` directory:

```bash
set -eu
SSHDOCK_SOURCE=/path/to/sshdock
APP_ROOT=/path/to/my-app
test -d "$APP_ROOT"
test -f "$SSHDOCK_SOURCE/.agents/skills/sshdock-operations/SKILL.md"
mkdir -p "$APP_ROOT/.agents/skills"
test ! -e "$APP_ROOT/.agents/skills/sshdock-operations"
test ! -L "$APP_ROOT/.agents/skills/sshdock-operations"
cp -R "$SSHDOCK_SOURCE/.agents/skills/sshdock-operations" "$APP_ROOT/.agents/skills/"
```

Start Codex from the app repository and invoke `$sshdock-operations`, or select it from `/skills`. Codex can also select it when the task matches its description. Check the loaded path if another skill has the same name. [Official Codex skill discovery and invocation](https://learn.chatgpt.com/docs/build-skills).

For **Claude Code**, use `.claude/skills` with the identical source:

```bash
set -eu
SSHDOCK_SOURCE=/path/to/sshdock
APP_ROOT=/path/to/my-app
test -d "$APP_ROOT"
test -f "$SSHDOCK_SOURCE/.agents/skills/sshdock-operations/SKILL.md"
mkdir -p "$APP_ROOT/.claude/skills"
test ! -e "$APP_ROOT/.claude/skills/sshdock-operations"
test ! -L "$APP_ROOT/.claude/skills/sshdock-operations"
cp -R "$SSHDOCK_SOURCE/.agents/skills/sshdock-operations" "$APP_ROOT/.claude/skills/"
```

Start a new local Claude Code session in that app repo and invoke `/sshdock-operations`. Automatic selection is also possible. Check for a personal/enterprise skill with the same name if the project copy is shadowed. The package uses standard `name`/`description` frontmatter without vendor-specific hooks, tool grants or substitutions. [Claude Code project skills](https://code.claude.com/docs/en/skills#choose-where-skills-load), [portable frontmatter](https://code.claude.com/docs/en/skills#using-skill-frontmatter-outside-claude-code).

Other agents can read `SKILL.md` directly and follow its commands. This describes local repository integrations; it does not claim cloud-session discovery or grant additional permissions. Keep installed copies under your own project policy and update them deliberately when upgrading SSHDock.

## First request: inspect only

Give the agent the intended app, operator identity and a verified SSH host-key setup privately. For example:

> Use sshdock-operations to inspect my-app on the supplied operator connection. Run read-only app, deployment and route queries. Report desired main, latest attempt, last success, service health and uncertainty separately. Do not deploy, read config values or change data.

Both integrations should use this same request shape, with no model-specific API:

```bash
ssh -T -o StrictHostKeyChecking=yes "$OPERATOR" apps list --json
ssh -T -o StrictHostKeyChecking=yes "$OPERATOR" apps health my-app --json
ssh -T -o StrictHostKeyChecking=yes "$OPERATOR" deployments list my-app --json
ssh -T -o StrictHostKeyChecking=yes "$OPERATOR" domains check my-app --json
```

Require schema `"1"`, the expected command, process exit and `error`; inspect result fields rather than interpreting exit `0` as app availability. A missing app is a typed `not_found` error. A warning/unknown preflight or route observation is not a successful runtime/ingress test. See [structured inspection](JSON_OUTPUT.md) and [read-only preflight](PREFLIGHT.md).

## Deployment and recovery requests

Supply the trusted local source repository, app and exact scope of permission. The skill covers first deploy, exact-ID observation, failure diagnosis and reviewed fixes. For example:

> Deploy the reviewed commit from this repository to my-app. Inspect preflight first, push through the configured SSHDock remote, then verify the new exact deployment ID and service/route/public response. If it fails, diagnose that attempt and present a concrete recovery proposal before changing history or data.

For recovery, the proposal must show current main, a locally verified compatible target, data/config/migration implications and the explicit expected-main lease. Human approval precedes the force-push; stale/busy rejection stops the workflow. The skill follows [manual Git-selected recovery](RECOVERY.md), including failed recovery and same-target redeploy. It never treats historical success as verification of a new attempt.

## Backup and restore requests

Give exact source resources, maintenance scope, storage and intended destination. For example:

> Prepare manual backups of my-app's named file volume and PostgreSQL database, plus separate SSHDock host state. Identify writers, consistency limits and secret-handling needs. Present archive/destination details and obtain approval before restoring. Verify from backups after the disposable source volumes are removed, including old data, new writes and persistence.

The skill follows [application-data backup and restore](APPLICATION_DATA_BACKUPS.md): stopped-writer file export, offline inspection, fresh-volume restore, PostgreSQL-native custom dump/atomic SQL restore, and separate host metadata. It does not create schedules, hosted storage or an unattended destructive recovery loop.

Workflow approval is a rule for the agent, **not enforcement by SSHDock**. A deploy credential can submit privileged Compose and a trusted operator can execute container commands. SSHDock assumes the owner trusts the credential holder with the single node. Keep host-key validation, secret storage, resource selection and data-compatibility review with that owner.

## Maintenance and verification

When CLI syntax, JSON/error semantics or recovery/backup behavior changes, update the skill and matching public docs in the same change. Keep one canonical package; installed copies are not automatically synchronized.

`make ci` executes the skill's `sshdock-read` examples against fake-backed shipped CLI handlers and validates JSON/query semantics. Existing preflight, recovery and backup tests cover their command implementations; real agent-driven VPS journeys are the separate release-acceptance gate. A parser/command test does not prove a model discovered the right skill or safely completed a live workflow. See [testing](TESTING.md) and [v1.1 acceptance](V1_1_ACCEPTANCE.md).

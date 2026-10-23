# Manual deployment recovery

This guide uses the **v1.1.0** structured commands and `jq` on your workstation. Run it from the app's trusted local Git repository. The Git receive account supports pushes, not fetch or `git ls-remote`.

Remote Git `main` selects the desired revision. A failed Compose update may already have replaced healthy containers; selecting an earlier revision creates another ordinary deployment attempt. It can also fail. There is no automatic rollback, traffic switch, or zero-downtime guarantee.

Volumes are preserved, but old code may be incompatible with current data or config. Review database migrations, external resources, image availability and stored config before proceeding. Restoring Git does not reverse those changes. Take an appropriate application-data backup before a destructive migration.

## Identify the desired revision and recovery target

Capture one health snapshot so the expected remote revision and historical success refer to the same observation:

```bash
set -eu
OPERATOR=sshdock@sshdock.example.com
APP=my-app
STATUS=$(ssh -T "$OPERATOR" apps health "$APP" --json)
printf '%s\n' "$STATUS" | jq -e '.schema_version == "1" and .command == "apps.health" and .error == null' >/dev/null
printf '%s\n' "$STATUS" | jq '.data | {current_main_commit, latest_deployment_id, latest_deployment_commit, latest_deployment_status, last_successful_deployment_id, last_successful_deployment_commit}'
EXPECTED_MAIN=$(printf '%s\n' "$STATUS" | jq -er '.data.current_main_commit | select(length > 0)')
BEFORE_ATTEMPT=$(printf '%s\n' "$STATUS" | jq -er '.data.latest_deployment_id | select(length > 0)')
LATEST_STATE=$(printf '%s\n' "$STATUS" | jq -r '.data.latest_deployment_status')
case "$LATEST_STATE" in
  succeeded|failed) ;;
  *) printf 'Stop: inspect the unfinished or unknown deployment first.\n' >&2; exit 1 ;;
esac
TARGET_REF=$(printf '%s\n' "$STATUS" | jq -er '.data.last_successful_deployment_commit | select(length > 0)')
TARGET_COMMIT=$(git rev-parse --verify "$TARGET_REF^{commit}")
git show --no-patch --format=fuller "$TARGET_COMMIT"
git diff --stat "$EXPECTED_MAIN" "$TARGET_COMMIT" --
```

The last successful attempt is a candidate, not evidence its containers are still serving or its revision remains compatible. To choose another reviewed local commit or tag, replace the `TARGET_REF` assignment; `^{commit}` also resolves annotated tags. If either Git object is missing locally, obtain it from a trusted source before continuing. No recorded success or no known-compatible target means stop and investigate.

If `TARGET_COMMIT` already equals `EXPECTED_MAIN`, skip the lease/push section. Fix the failing prerequisite and explicitly run `ssh "$OPERATOR" apps redeploy "$APP"`, then `ssh "$OPERATOR" deployments list "$APP" --json`. Redeploy is synchronous and prints no queued ID: copy its new `redeploy` attempt ID by matching the target commit and invocation time, excluding the earlier IDs. Continue at exact-ID verification below. An unchanged-ref Git push creates no new attempt.

## Confirm and update remote main with a lease

Display both full commits and require typing the target. The explicit expected SHA protects against overwriting a newer remote `main` without relying on a fetched tracking ref:

```bash
test "$TARGET_COMMIT" != "$EXPECTED_MAIN"
printf 'App: %s\nExpected remote main: %s\nRecovery target: %s\n' "$APP" "$EXPECTED_MAIN" "$TARGET_COMMIT"
printf 'Type target commit %s to confirm: ' "$TARGET_COMMIT"
read -r CONFIRM_COMMIT
if test "$CONFIRM_COMMIT" != "$TARGET_COMMIT"; then
  printf 'Recovery cancelled.\n' >&2
  exit 1
fi
git push --force-with-lease="refs/heads/main:$EXPECTED_MAIN" \
  sshdock "$TARGET_COMMIT:refs/heads/main"
```

On a stale lease or busy-app rejection, stop and inspect again. Do not automatically refresh the expected SHA or fall back to `--force`. These commands retain the normal Git-push deployment contract; they do not create a second desired state.

## Verify the new attempt and availability separately

For a Git push, copy the new `dep_...` ID from its `deploy: queued` output, which may be on stderr. If detached, inspect deployment history and match the new ID, target commit and push time. For the same-target redeploy branch, use the new ID identified above. Never reuse the target commit's historical successful attempt or assume an unrelated latest attempt belongs to this operation.

```bash
RECOVERY_ATTEMPT=dep_replace_with_the_new_attempt_id
test "$RECOVERY_ATTEMPT" != "$BEFORE_ATTEMPT"
ssh -T "$OPERATOR" deployments logs "$APP" "$RECOVERY_ATTEMPT" -f
RESULT=$(ssh -T "$OPERATOR" deployments info "$APP" "$RECOVERY_ATTEMPT" --json)
printf '%s\n' "$RESULT" | jq -e --arg id "$RECOVERY_ATTEMPT" --arg target "$TARGET_COMMIT" \
  '.schema_version == "1" and .error == null and .data.id == $id and .data.commit_sha == $target and .data.status == "succeeded"'
ssh -T "$OPERATOR" apps health "$APP" --json
ssh -T "$OPERATOR" domains check "$APP" --json
curl --fail --show-error https://my-app.example.com/
```

Inspect service health and route result fields; a successful JSON query only confirms the query completed. Verify the application's expected revision, data reads and new writes separately. For bounded automated polling, use the [exact-ID polling example](JSON_OUTPUT.md#ci-polling-example).

A failed recovery leaves remote `main` at the selected target, records a new failed attempt, and retains the earlier successful attempt. The two attempts can have the same commit and release ID: compare deployment IDs and statuses. Read that failed attempt's logs/detail, correct the prerequisite, then explicitly `apps redeploy` current `main` and verify its new ID. Daemon restart does not silently replay active failures.

The [Next.js recovery lab](../examples/labs/failed-deploy-and-git-recovery/README.md) demonstrates a build failure before replacement. Docker-gated recovery tests additionally exercise an unhealthy Compose replacement, failed recovery and preserved named-volume data; run `SSHDOCK_E2E_DOCKER=1 make recovery-e2e` on a disposable Docker test host.

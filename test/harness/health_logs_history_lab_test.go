package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHealthLogsAndHistoryFeatureLab_acceptanceScriptCompletesInspectionAndRedeploy(t *testing.T) {
	// Given a recovered app with one failed and one successful Git-push attempt.
	output, err := runHealthLogsAndHistoryLab(t, false, false)

	// When the lab inspects it and explicitly redeploys the current main commit.

	// Then it preserves two releases and adds one successful redeploy attempt.
	if err != nil {
		t.Fatalf("acceptance script: %v\n%s", err, output)
	}
	if !strings.Contains(output, "dep_redeploy\tsucceeded\tredeploy\tgood") {
		t.Fatalf("acceptance script output missing redeploy attempt:\n%s", output)
	}
}

func TestHealthLogsAndHistoryFeatureLab_acceptanceScriptRejectsReleaseCreatedByRedeploy(t *testing.T) {
	// Given a recovered app whose redeploy adds a new release row.
	output, err := runHealthLogsAndHistoryLab(t, true, false)

	// When the lab checks the release history after redeploy.

	// Then it fails instead of accepting a new release for the same Git commit.
	if err == nil {
		t.Fatalf("acceptance script accepted a release created by redeploy:\n%s", output)
	}
}

func TestHealthLogsAndHistoryFeatureLab_acceptanceScriptAllowsQuietLogFollow(t *testing.T) {
	// Given an app whose live follow stream does not flush a line before it stops.
	output, err := runHealthLogsAndHistoryLab(t, false, true)

	// When the lab observes the bounded follow lifecycle.

	// Then it continues to the redeploy and cleanup checks.
	if err != nil {
		t.Fatalf("acceptance script: %v\n%s", err, output)
	}
}

func runHealthLogsAndHistoryLab(t *testing.T, extraRelease bool, quietFollow bool) (string, error) {
	t.Helper()
	root := repoRoot(t)
	fakeBin := t.TempDir()
	stateDir := t.TempDir()
	sshPath := filepath.Join(fakeBin, "ssh")
	const fakeSSH = `#!/usr/bin/env bash
set -euo pipefail

command="$*"
state="${FAKE_STATE_DIR:?}"
route="${FAKE_ROUTE_HOST:?}"

case "$command" in
  *'apps health'*)
    if [[ -e "$state/redeployed" ]]; then
      if [[ -e "$state/redeploy-health-checked" ]]; then
        printf 'app: failed-deploy-and-git-recovery\nhealth: ok\ncurrent main: good\nlatest deploy: dep_redeploy succeeded commit=good trigger=redeploy\nroutes: 1 active, 0 attention\nlast failure: dep_bad stage=build\nok\trestart policy\tconfigured for routed and running services\n'
      else
        touch "$state/redeploy-health-checked"
        printf 'app: failed-deploy-and-git-recovery\nhealth: warn\ncurrent main: good\nlatest deploy: dep_redeploy deploying commit=good trigger=redeploy\nroutes: 1 active, 0 attention\nlast failure: dep_bad stage=build\nok\trestart policy\tconfigured for routed and running services\n'
      fi
    else
      printf 'app: failed-deploy-and-git-recovery\nhealth: ok\ncurrent main: good\nlatest deploy: dep_good succeeded commit=good trigger=push\nroutes: 1 active, 0 attention\nlast failure: dep_bad stage=build\nok\trestart policy\tconfigured for routed and running services\n'
    fi
    ;;
  *'domains list'*)
    printf '%s\tweb\t18100\ttrue\n' "$route"
    ;;
  *'domains check'*)
    printf '%s\tweb\t18100\ttrue\tok\tactive Caddy route matches\n' "$route"
    ;;
  *'releases list'*)
    printf 'rel_good\tsucceeded\tgood\nrel_bad\tfailed\tbad\n'
    if [[ ${FAKE_EXTRA_RELEASE:-} == true && -e "$state/redeployed" ]]; then
      printf 'rel_extra\tsucceeded\textra\n'
    fi
    ;;
  *'deployments list'*)
    printf 'dep_bad\tfailed\tpush\tbad\n'
    printf 'dep_good\tsucceeded\tpush\tgood\n'
    if [[ -e "$state/redeployed" ]]; then
      printf 'dep_redeploy\tsucceeded\tredeploy\tgood\n'
    fi
    ;;
  *'events list'*)
    printf '2026-07-25T00:00:00Z\tdeploy.failed\tbuild failed\n'
    printf '2026-07-25T00:01:00Z\tdeploy.succeeded\tdeployment succeeded\n'
    ;;
  *'logs '* )
    if [[ "$command" == *' -f' && ${FAKE_QUIET_FOLLOW:-} == true ]]; then
      exit 0
    fi
    printf 'nextjs log line\n'
    ;;
  *'apps redeploy'*)
    touch "$state/redeployed"
    ;;
  *'apps remove'*)
    ;;
  *)
    printf 'unexpected SSH command: %s\n' "$command" >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(sshPath, []byte(fakeSSH), 0o755); err != nil {
		t.Fatalf("WriteFile fake ssh: %v", err)
	}
	curlPath := filepath.Join(fakeBin, "curl")
	if err := os.WriteFile(curlPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile fake curl: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_STATE_DIR", stateDir)
	t.Setenv("FAKE_ROUTE_HOST", "failed-deploy-and-git-recovery.example.com")
	t.Setenv("FAKE_EXTRA_RELEASE", strconv.FormatBool(extraRelease))
	t.Setenv("FAKE_QUIET_FOLLOW", strconv.FormatBool(quietFollow))
	t.Setenv("SSHDOCK_TARGET", "sshdock@example.com")
	t.Setenv("SSHDOCK_APP", "failed-deploy-and-git-recovery")
	t.Setenv("SSHDOCK_ROUTE_HOST", os.Getenv("FAKE_ROUTE_HOST"))
	t.Setenv("SSHDOCK_EXPECTED_MAIN", "good")
	t.Setenv("SSHDOCK_FAILED_MAIN", "bad")
	command := exec.Command("bash", filepath.Join(root, "examples", "labs", "health-logs-and-history", "acceptance.sh"))
	output, err := command.CombinedOutput()
	return string(output), err
}

package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDomainsAndRouteCheckFeatureLab_acceptanceScriptCompletesRouteLifecycle(t *testing.T) {
	// Given the tracked lab script with a hermetic SSHDock route surface.
	output, err := runDomainsAndRouteCheckLab(t, "ok")

	// When the lab exercises its automatic and manual route lifecycle.

	// Then it accepts the active route checks and removes the final route.
	if err != nil {
		t.Fatalf("acceptance script: %v\n%s", err, output)
	}
	if !strings.Contains(output, "no domains") {
		t.Fatalf("acceptance script output = %q, want final route removal", output)
	}
}

func TestDomainsAndRouteCheckFeatureLab_acceptanceScriptRejectsMissingManualRoute(t *testing.T) {
	// Given a route check where the automatic route is active but the manual route is missing.
	output, err := runDomainsAndRouteCheckLab(t, "missing")

	// When the script verifies the manual attachment.

	// Then it fails instead of accepting the automatic route's healthy row.
	if err == nil {
		t.Fatalf("acceptance script succeeded with a missing manual route:\n%s", output)
	}
}

func runDomainsAndRouteCheckLab(t *testing.T, manualStatus string) (string, error) {
	t.Helper()
	root := repoRoot(t)
	fakeBin := t.TempDir()
	stateDir := t.TempDir()
	sshPath := filepath.Join(fakeBin, "ssh")
	const fakeSSH = `#!/usr/bin/env bash
set -euo pipefail

command="$*"
auto="${FAKE_AUTO_ROUTE:?}"
manual="${FAKE_MANUAL_ROUTE:?}"
state="${FAKE_STATE_DIR:?}"
manual_status="${FAKE_MANUAL_STATUS:?}"
route_line() { printf '%s\tweb\t18200\ttrue\tok\tactive Caddy route matches\n' "$1"; }

case "$command" in
  *SSHDOCK_CADDY_ADMIN_ADDRESS=127.0.0.1:1*)
    printf '%s\tweb\t18200\ttrue\tunavailable\tactive Caddy check failed\n' "$auto"
    if [[ -e "$state/manual" ]]; then
      printf '%s\tweb\t18200\ttrue\tunavailable\tactive Caddy check failed\n' "$manual"
    fi
    ;;
  *'domains attach'*)
    touch "$state/manual"
    ;;
  *'domains detach'*)
    if [[ "$command" == *"$manual"* ]]; then
      rm -f "$state/manual"
    else
      touch "$state/final-detached"
    fi
    ;;
  *'domains list'*)
    if [[ -e "$state/final-detached" ]]; then
      printf 'no domains\n'
    else
      printf '%s\tweb\t18200\ttrue\n' "$auto"
    fi
    ;;
  *'domains check'*)
    route_line "$auto"
    if [[ -e "$state/manual" ]]; then
      if [[ "$manual_status" == "ok" ]]; then
        route_line "$manual"
      else
        printf '%s\tweb\t18200\ttrue\tmissing\tactive Caddy route missing\n' "$manual"
      fi
    fi
    ;;
  *'caddy validate'*)
    printf 'Valid configuration\n'
    ;;
  *'apps health'*|*'apps remove'*)
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
	t.Setenv("FAKE_AUTO_ROUTE", "domains-and-route-check.example.com")
	t.Setenv("FAKE_MANUAL_ROUTE", "manual-domains-and-route-check.example.com")
	t.Setenv("FAKE_MANUAL_STATUS", manualStatus)
	t.Setenv("FAKE_STATE_DIR", stateDir)
	t.Setenv("SSHDOCK_TARGET", "sshdock@example.com")
	t.Setenv("SSHDOCK_ADMIN_TARGET", "admin@example.com")
	t.Setenv("SSHDOCK_AUTO_ROUTE_HOST", os.Getenv("FAKE_AUTO_ROUTE"))
	t.Setenv("SSHDOCK_MANUAL_ROUTE_HOST", os.Getenv("FAKE_MANUAL_ROUTE"))
	command := exec.Command("bash", filepath.Join(root, "examples", "labs", "domains-and-route-check", "acceptance.sh"))
	output, err := command.CombinedOutput()
	return string(output), err
}

//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Ordinary binaries exercise DockerRunner; only the external Docker process is stubbed.
func writeDockerFixture(t *testing.T, binDir string) {
	t.Helper()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
fail() { printf 'unexpected fixture docker command: %s\n' "$*" >&2; exit 1; }
if [ -n "${SSHDOCK_TEST_DOCKER_RECORD:-}" ]; then
  printf '%s\n' "$*" >> "$SSHDOCK_TEST_DOCKER_RECORD"
fi
if [ "$*" = 'info --format {{.ServerVersion}}' ]; then
  printf 'test-engine\n'
  exit 0
fi
[ "$#" -gt 0 ] && [ "$1" = compose ] || fail "$@"
shift
if [ "$*" = version ]; then
  printf 'Docker Compose test fixture\n'
  exit 0
fi
[ "$#" -ge 5 ] && [ "$1" = -f ] && [ "$3" = -p ] || fail "$@"
shift 4
case "$*" in
  config) exit 0 ;;
  'config --format json')
    if [ -n "${SSHDOCK_TEST_DOCKER_MODEL:-}" ]; then
      printf '%s\n' "$SSHDOCK_TEST_DOCKER_MODEL"
    else
      printf '%s\n' '{"services":{"web":{"image":"example/web:latest","restart":"unless-stopped"}}}'
    fi
    ;;
  'pull --ignore-buildable'|build|stop|restart|'down --remove-orphans') ;;
  restart\ *) [ "$#" -eq 2 ] || fail "$@" ;;
  'up -d --wait --wait-timeout 120')
    if [ -n "${SSHDOCK_TEST_DOCKER_DEPLOY_DELAY:-}" ]; then sleep "$SSHDOCK_TEST_DOCKER_DEPLOY_DELAY"; fi
    ;;
  start)
    if [ -n "${SSHDOCK_TEST_DOCKER_START_ERROR:-}" ]; then
      printf '%s\n' "$SSHDOCK_TEST_DOCKER_START_ERROR" >&2
      exit 1
    fi
    ;;
  'ps --all --format json')
    if [ -n "${SSHDOCK_TEST_DOCKER_SERVICES:-}" ]; then
      printf '%s\n' "$SSHDOCK_TEST_DOCKER_SERVICES"
    else
      printf '%s\n' '[{"Service":"web","State":"running"}]'
    fi
    ;;
  logs|logs\ *)
    shift
    if [ "${1:-}" = --follow ]; then shift; fi
    if [ "${1:-}" = --tail ]; then
      [ "$#" -ge 2 ] || fail "$@"
      case "$2" in ''|*[!0-9]*) fail "$@" ;; esac
      shift 2
    fi
    [ "$#" -le 1 ] || fail "$@"
    case "${1:-}" in -*) fail "$@" ;; esac
    printf '%s' "${SSHDOCK_TEST_DOCKER_LOGS:-first-dashboard-log}"
    ;;
  'exec -T -- '*|'exec -- '*) printf 'exec-output' ;;
  'run --rm -T -- '*|'run --rm -- '*) printf 'run-output' ;;
  *) fail "$@" ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDockerFixtureRejectsUnexpectedCommands(t *testing.T) {
	binDir := t.TempDir()
	writeDockerFixture(t, binDir)
	for _, args := range [][]string{
		{"system", "prune", "--all"},
		{"compose", "-f", "compose.yml", "-p", "sshdock_fixture", "up", "-d"},
		{"compose", "-f", "compose.yml", "-p", "sshdock_fixture", "logs", "--unknown"},
	} {
		output, err := exec.Command(filepath.Join(binDir, "docker"), args...).CombinedOutput()
		if err == nil || !strings.Contains(string(output), "unexpected fixture docker command") {
			t.Fatalf("accepted unexpected command %v: err=%v output=%s", args, err, output)
		}
	}
}

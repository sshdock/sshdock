//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHardeningBootstrapUpgradePreservesDataAndDiagnostics(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()

	sourceBinDir := filepath.Join(tmp, "source-bin")
	if err := os.MkdirAll(sourceBinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source bin: %v", err)
	}
	runCommand(t, root, nil, "go", "build", "-o", filepath.Join(sourceBinDir, "sshdock"), "./cmd/sshdock")
	runCommand(t, root, nil, "go", "build", "-o", filepath.Join(sourceBinDir, "sshdockd"), "./cmd/sshdockd")

	fakeBinDir := filepath.Join(tmp, "fake-bin")
	fakeLogPath := filepath.Join(tmp, "fake-commands.log")
	writeHardeningFakeCommands(t, fakeBinDir)

	installRoot := filepath.Join(tmp, "root")
	bootstrapEnv := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_TAG=test-local",
		"SSHDOCK_BOOTSTRAP_ROOT="+installRoot,
		"SSHDOCK_BOOTSTRAP_SOURCE_BIN_DIR="+sourceBinDir,
		"SSHDOCK_BOOTSTRAP_SKIP_CHOWN=1",
		"SSHDOCK_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
	)

	runCommand(t, root, bootstrapEnv, "bash", "scripts/bootstrap.sh")
	sentinelPath := filepath.Join(installRoot, "var/lib/sshdock/apps/preserve.txt")
	if err := os.WriteFile(sentinelPath, []byte("keep me\n"), 0o644); err != nil {
		t.Fatalf("WriteFile sentinel: %v", err)
	}
	staleBinary := filepath.Join(installRoot, "usr/local/bin/sshdock")
	if err := os.WriteFile(staleBinary, []byte("#!/bin/sh\necho stale\n"), 0o755); err != nil {
		t.Fatalf("WriteFile stale binary: %v", err)
	}

	runCommand(t, root, bootstrapEnv, "bash", "scripts/bootstrap.sh")

	if got := readFile(t, sentinelPath); got != "keep me\n" {
		t.Fatalf("sentinel = %q", got)
	}
	versionOutput := runCommand(t, root, nil, filepath.Join(installRoot, "usr/local/bin/sshdock"), "version")
	if strings.Contains(versionOutput, "stale") || !strings.Contains(versionOutput, "sshdock") {
		t.Fatalf("installed sshdock version output = %q", versionOutput)
	}

	dataDir := filepath.Join(installRoot, "var/lib/sshdock")
	for _, path := range []string{
		filepath.Join(dataDir, "git/.ssh/authorized_keys"),
		filepath.Join(dataDir, ".ssh/authorized_keys"),
	} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("WriteFile authorized_keys: %v", err)
		}
	}
	diagnosticsCaddyMainPath := filepath.Join(tmp, "diagnostics-Caddyfile")
	if err := os.WriteFile(diagnosticsCaddyMainPath, []byte("import "+filepath.Join(installRoot, "etc/caddy/sshdock.caddyfile")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile diagnostics caddy main: %v", err)
	}
	diagnosticsEnv := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_SQLITE_DB_PATH="+filepath.Join(dataDir, "sshdock.db"),
		"SSHDOCK_APPS_DIR="+filepath.Join(dataDir, "apps"),
		"SSHDOCK_GIT_HOME_DIR="+filepath.Join(dataDir, "git"),
		"SSHDOCK_GIT_AUTHORIZED_KEYS_PATH="+filepath.Join(dataDir, "git/.ssh/authorized_keys"),
		"SSHDOCK_OPERATOR_HOST_KEY_PATH="+filepath.Join(dataDir, "ssh_host_rsa_key"),
		"SSHDOCK_OPERATOR_AUTHORIZED_KEYS_PATH="+filepath.Join(dataDir, ".ssh/authorized_keys"),
		"SSHDOCK_CADDY_CONFIG_PATH="+filepath.Join(installRoot, "etc/caddy/sshdock.caddyfile"),
		"SSHDOCK_CADDY_MAIN_CONFIG_PATH="+diagnosticsCaddyMainPath,
	)
	runCommand(t, root, diagnosticsEnv, filepath.Join(installRoot, "usr/local/bin/sshdock"), "server", "domain", "set", "example.com")
	diagnosticsOutput := runCommand(t, root, diagnosticsEnv, filepath.Join(installRoot, "usr/local/bin/sshdock"), "diagnostics")
	for _, want := range []string{
		"ok config",
		"ok operating system",
		"ok docker",
		"ok docker compose",
		"ok caddy",
		"ok ssh",
		"ok sshd",
		"ok git",
		"ok systemd",
		"ok sshdockd service",
		"ok port 22",
		"ok port 80",
		"ok port 443",
		"ok base-domain DNS",
		"ok wildcard DNS",
		"ok caddy import",
		"ok caddy config",
		"ok git authorized_keys",
		"ok operator authorized_keys",
		"ok sqlite migrations",
		"diagnostics ok",
	} {
		if !strings.Contains(diagnosticsOutput, want) {
			t.Fatalf("diagnostics output missing %q:\n%s", want, diagnosticsOutput)
		}
	}
}

func writeHardeningFakeCommands(t *testing.T, fakeBinDir string) {
	t.Helper()
	writeFakeCommand(t, fakeBinDir, "docker", `#!/bin/sh
printf 'docker %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "caddy", `#!/bin/sh
printf 'caddy %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "systemctl", `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "id", `#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = "-u" ]; then
	echo 0
	exit 0
fi
printf 'id %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 1
`)
	writeFakeCommand(t, fakeBinDir, "useradd", `#!/bin/sh
printf 'useradd %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "usermod", `#!/bin/sh
printf 'usermod %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "sudo", `#!/bin/sh
printf 'sudo %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "visudo", `#!/bin/sh
printf 'visudo %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "ssh", `#!/bin/sh
echo OpenSSH_fake
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "sshd", `#!/bin/sh
echo OpenSSH_fake
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "git", `#!/bin/sh
echo git version fake
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "ss", `#!/bin/sh
echo 'LISTEN 0 4096 *:22 *:*'
echo 'LISTEN 0 4096 *:80 *:*'
echo 'LISTEN 0 4096 *:443 *:*'
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "getent", `#!/bin/sh
echo '203.0.113.10 STREAM '"$2"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "uname", `#!/bin/sh
if [ "$1" = "-s" ]; then
	echo Linux
	exit 0
fi
/usr/bin/uname "$@"
`)
}

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
	runCommand(t, root, nil, "go", "build", "-o", filepath.Join(sourceBinDir, "rhumbase"), "./cmd/rhumbase")
	runCommand(t, root, nil, "go", "build", "-o", filepath.Join(sourceBinDir, "rhumbased"), "./cmd/rhumbased")

	fakeBinDir := filepath.Join(tmp, "fake-bin")
	fakeLogPath := filepath.Join(tmp, "fake-commands.log")
	writeHardeningFakeCommands(t, fakeBinDir)

	installRoot := filepath.Join(tmp, "root")
	bootstrapEnv := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_TAG=test-local",
		"RHUMBASE_BOOTSTRAP_ROOT="+installRoot,
		"RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR="+sourceBinDir,
		"RHUMBASE_BOOTSTRAP_SKIP_CHOWN=1",
		"RHUMBASE_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
	)

	runCommand(t, root, bootstrapEnv, "bash", "scripts/bootstrap.sh")
	sentinelPath := filepath.Join(installRoot, "var/lib/rhumbase/apps/preserve.txt")
	if err := os.WriteFile(sentinelPath, []byte("keep me\n"), 0o644); err != nil {
		t.Fatalf("WriteFile sentinel: %v", err)
	}
	staleBinary := filepath.Join(installRoot, "usr/local/bin/rhumbase")
	if err := os.WriteFile(staleBinary, []byte("#!/bin/sh\necho stale\n"), 0o755); err != nil {
		t.Fatalf("WriteFile stale binary: %v", err)
	}

	runCommand(t, root, bootstrapEnv, "bash", "scripts/bootstrap.sh")

	if got := readFile(t, sentinelPath); got != "keep me\n" {
		t.Fatalf("sentinel = %q", got)
	}
	versionOutput := runCommand(t, root, nil, filepath.Join(installRoot, "usr/local/bin/rhumbase"), "version")
	if strings.Contains(versionOutput, "stale") || !strings.Contains(versionOutput, "rhumbase") {
		t.Fatalf("installed rhumbase version output = %q", versionOutput)
	}

	dataDir := filepath.Join(installRoot, "var/lib/rhumbase")
	diagnosticsEnv := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
		"RHUMBASE_DATA_DIR="+dataDir,
		"RHUMBASE_SQLITE_DB_PATH="+filepath.Join(dataDir, "rhumbase.db"),
		"RHUMBASE_APPS_DIR="+filepath.Join(dataDir, "apps"),
		"RHUMBASE_GIT_HOME_DIR="+filepath.Join(dataDir, "git"),
		"RHUMBASE_GIT_AUTHORIZED_KEYS_PATH="+filepath.Join(dataDir, "git/.ssh/authorized_keys"),
		"RHUMBASE_DASHBOARD_HOST_KEY_PATH="+filepath.Join(dataDir, "dashboard/ssh_host_rsa_key"),
		"RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH="+filepath.Join(dataDir, "dashboard/.ssh/authorized_keys"),
		"RHUMBASE_CADDY_CONFIG_PATH="+filepath.Join(installRoot, "etc/caddy/rhumbase.caddyfile"),
	)
	diagnosticsOutput := runCommand(t, root, diagnosticsEnv, filepath.Join(installRoot, "usr/local/bin/rhumbase"), "diagnostics")
	for _, want := range []string{
		"ok config",
		"ok docker",
		"ok docker compose",
		"ok caddy",
		"ok ssh",
		"ok sshd",
		"ok git",
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
printf 'docker %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "caddy", `#!/bin/sh
printf 'caddy %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "systemctl", `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "id", `#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = "-u" ]; then
	echo 0
	exit 0
fi
printf 'id %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 1
`)
	writeFakeCommand(t, fakeBinDir, "useradd", `#!/bin/sh
printf 'useradd %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "usermod", `#!/bin/sh
printf 'usermod %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "sudo", `#!/bin/sh
printf 'sudo %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "visudo", `#!/bin/sh
printf 'visudo %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
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
}

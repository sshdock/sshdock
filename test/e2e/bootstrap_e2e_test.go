//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapInstallsBinariesDataDirsAndService(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()

	sourceBinDir := filepath.Join(tmp, "source-bin")
	if err := os.MkdirAll(sourceBinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source bin: %v", err)
	}
	rhumbasePath := filepath.Join(sourceBinDir, "rhumbase")
	rhumbasedPath := filepath.Join(sourceBinDir, "rhumbased")
	runCommand(t, root, nil, "go", "build", "-o", rhumbasePath, "./cmd/rhumbase")
	runCommand(t, root, nil, "go", "build", "-o", rhumbasedPath, "./cmd/rhumbased")

	fakeBinDir := filepath.Join(tmp, "fake-bin")
	fakeLogPath := filepath.Join(tmp, "fake-commands.log")
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

	installRoot := filepath.Join(tmp, "root")
	env := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_TAG=test-local",
		"RHUMBASE_BOOTSTRAP_ROOT="+installRoot,
		"RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR="+sourceBinDir,
		"RHUMBASE_BOOTSTRAP_SKIP_CHOWN=1",
		"RHUMBASE_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
	)

	runCommand(t, root, env, "bash", "scripts/bootstrap.sh")

	assertExecutable(t, filepath.Join(installRoot, "usr/local/bin/rhumbase"))
	assertExecutable(t, filepath.Join(installRoot, "usr/local/bin/rhumbased"))
	assertDir(t, filepath.Join(installRoot, "var/lib/rhumbase"))
	assertDir(t, filepath.Join(installRoot, "var/lib/rhumbase/apps"))

	unitPath := filepath.Join(installRoot, "etc/systemd/system/rhumbased.service")
	unit := readFile(t, unitPath)
	for _, want := range []string{
		"After=network-online.target docker.service",
		"Requires=docker.service",
		"User=rhumbase",
		"Group=rhumbase",
		"Environment=RHUMBASE_DATA_DIR=/var/lib/rhumbase",
		"Environment=RHUMBASE_GIT_HOST=server",
		"Environment=RHUMBASE_COMPOSE_RUNNER=docker",
		"Environment=RHUMBASE_CADDY_CONFIG_PATH=/etc/caddy/rhumbase.caddyfile",
		"ExecStart=/usr/local/bin/rhumbased",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("service unit missing %q:\n%s", want, unit)
		}
	}

	fakeLog := readFile(t, fakeLogPath)
	for _, want := range []string{
		"docker version",
		"docker compose version",
		"caddy version",
		"systemctl --version",
		"useradd --system --home /var/lib/rhumbase --shell /usr/sbin/nologin rhumbase",
		"useradd --system --home /var/lib/rhumbase/git --shell /usr/bin/git-shell git",
		"systemctl daemon-reload",
		"systemctl enable --now rhumbased.service",
	} {
		if !strings.Contains(fakeLog, want) {
			t.Fatalf("fake command log missing %q:\n%s", want, fakeLog)
		}
	}
}

func TestBootstrapRequiresTag(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	env := append(os.Environ(),
		"RHUMBASE_BOOTSTRAP_ROOT="+filepath.Join(tmp, "root"),
		"RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR="+tmp,
		"RHUMBASE_BOOTSTRAP_SKIP_USER=1",
		"RHUMBASE_BOOTSTRAP_SKIP_CHOWN=1",
	)

	cmd := exec.Command("bash", "scripts/bootstrap.sh")
	cmd.Dir = root
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("bootstrap succeeded without RHUMBASE_TAG:\n%s", output)
	}
	if !strings.Contains(string(output), "RHUMBASE_TAG is required") {
		t.Fatalf("bootstrap missing tag output = %s", output)
	}
}

func writeFakeCommand(t *testing.T, dir string, name string, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll fake bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile fake command %s: %v", name, err)
	}
}

func assertExecutable(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat executable %s: %v", path, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s is not executable: %v", path, info.Mode().Perm())
	}
}

func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat dir %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(data)
}

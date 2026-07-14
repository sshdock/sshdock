//go:build e2e

package e2e

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/store"
)

func TestGitReceiveInvalidAppNameEndToEnd(t *testing.T) {
	// Given
	requireGit(t)
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}
	runCommand(t, root, nil, "go", "build", "-o", filepath.Join(binDir, "sshdockd"), "./cmd/sshdockd")

	fakeSSHPath := filepath.Join(binDir, "fake-ssh")
	writeGitBoundaryFakeSSH(t, fakeSSHPath)

	dataDir := filepath.Join(tmp, "data")
	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source: %v", err)
	}
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yaml"), []byte("services:\n  web:\n    image: example/web:latest\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	runGit(t, sourceDir, nil, "add", "compose.yaml")
	runGit(t, sourceDir, nil, "commit", "-m", "invalid app name boundary")
	runGit(t, sourceDir, nil, "remote", "add", "sshdock", "git@server:My_App.git")

	env := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_SSH="+fakeSSHPath,
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_COMPOSE_RUNNER=fake",
	)

	// When
	command := exec.Command("git", "push", "sshdock", "main")
	command.Dir = sourceDir
	command.Env = env
	output, err := command.CombinedOutput()

	// Then
	if err == nil {
		t.Fatalf("git push succeeded, want invalid app name rejection:\n%s", output)
	}
	for _, want := range []string{
		`app name "My_App" is not a normalized DNS label; use "my-app"`,
		"git remote set-url sshdock git@server:my-app.git",
	} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("git push output missing %q:\n%s", want, output)
		}
	}

	sqlite, openErr := store.OpenSQLite(context.Background(), filepath.Join(dataDir, "sshdock.db"))
	if openErr != nil {
		t.Fatalf("OpenSQLite: %v", openErr)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	if _, getErr := sqlite.GetApp(context.Background(), "My_App"); !errors.Is(getErr, store.ErrNotFound) {
		t.Fatalf("GetApp(My_App) error = %v, want ErrNotFound", getErr)
	}
	if _, getErr := sqlite.GetApp(context.Background(), "my-app"); !errors.Is(getErr, store.ErrNotFound) {
		t.Fatalf("GetApp(my-app) error = %v, want ErrNotFound", getErr)
	}
}

func writeGitBoundaryFakeSSH(t *testing.T, path string) {
	t.Helper()
	content := `#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
	case "$1" in
		-o|-p|-l|-i|-F|-S|-J|-b|-c|-m) shift 2 ;;
		-*) shift ;;
		*) break ;;
	esac
done
shift
SSH_ORIGINAL_COMMAND="$*" exec sshdockd git-receive
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile fake ssh: %v", err)
	}
}

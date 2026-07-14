//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealDashboardSSHScreenCapture(t *testing.T) {
	requireGit(t)
	sshdPath := requireCommandOrSkip(t, "sshd")
	sshPath := requireCommandOrSkip(t, "ssh")
	sshKeygenPath := requireCommandOrSkip(t, "ssh-keygen")

	paths := setupBootstrappedServerPush(t, "fake")
	appName := "dashboard-capture-app"
	pushComposeAppThroughSSH(t, paths, appName, map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	})

	server := startDashboardSSHServer(t, paths, sshdPath, sshKeygenPath)
	artifactDir := dashboardCaptureArtifactDir(t)

	manifest := captureDashboardSSHSession(t, dashboardCaptureOptions{
		SSHPath:     sshPath,
		Paths:       paths,
		Server:      server,
		AppName:     appName,
		ArtifactDir: artifactDir,
		Rows:        32,
		Cols:        120,
		Timeout:     20 * time.Second,
	})

	if manifest.Rows != 32 || manifest.Cols != 120 {
		t.Fatalf("manifest size = %dx%d, want 32x120", manifest.Rows, manifest.Cols)
	}
	if len(manifest.Frames) != 7 {
		t.Fatalf("manifest frames = %#v", manifest.Frames)
	}
	for _, frame := range manifest.Frames {
		textPath := filepath.Join(artifactDir, frame.TextPath)
		pngPath := filepath.Join(artifactDir, frame.PNGPath)
		text := readFile(t, textPath)
		for _, want := range []string{"SSHDock", appName} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q:\n%s", textPath, want, text)
			}
		}
		if info, err := os.Stat(pngPath); err != nil {
			t.Fatalf("stat %s: %v", pngPath, err)
		} else if info.Size() == 0 {
			t.Fatalf("%s is empty", pngPath)
		}
	}
	for _, rel := range []string{"session.ansi", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(artifactDir, rel)); err != nil {
			t.Fatalf("expected artifact %s: %v", rel, err)
		}
	}
	t.Logf("wrote real SSH dashboard screenshots to %s", artifactDir)
}

func dashboardCaptureArtifactDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("SSHDOCK_TUI_SCREENSHOT_DIR"); dir != "" {
		return dir
	}
	return filepath.Join("..", "..", "_artifacts", "tui-screenshots-real")
}

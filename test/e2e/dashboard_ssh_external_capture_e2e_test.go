//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type externalDashboardCaptureConfig struct {
	Args        []string
	ArtifactDir string
	Rows        int
	Cols        int
	Timeout     time.Duration
	MaxTabs     int
}

func TestExternalDashboardSSHScreenCapture(t *testing.T) {
	sshPath := requireCommandOrSkip(t, "ssh")
	config, ok, err := externalDashboardCaptureConfigFromEnv(os.Getenv)
	if err != nil {
		t.Fatalf("external dashboard capture config: %v", err)
	}
	if !ok {
		t.Skip("set SSHDOCK_TUI_SCREENSHOT_SSH_TARGET=dashboard@host to capture a real external dashboard")
	}

	manifest := captureDashboardSSHCommandSession(t, dashboardCommandCaptureOptions{
		SSHPath:     sshPath,
		Args:        config.Args,
		ArtifactDir: config.ArtifactDir,
		Rows:        config.Rows,
		Cols:        config.Cols,
		Timeout:     config.Timeout,
		CaptureTabs: true,
		MaxTabs:     config.MaxTabs,
	})
	if len(manifest.Frames) < 2 {
		t.Fatalf("manifest frames = %#v", manifest.Frames)
	}
	t.Logf("wrote external SSH dashboard screenshots to %s", config.ArtifactDir)
}

type getenvFunc func(string) string

func externalDashboardCaptureConfigFromEnv(getenv getenvFunc) (externalDashboardCaptureConfig, bool, error) {
	target := strings.TrimSpace(getenv("SSHDOCK_TUI_SCREENSHOT_SSH_TARGET"))
	if target == "" {
		return externalDashboardCaptureConfig{}, false, nil
	}

	rows, err := intFromEnv(getenv, "SSHDOCK_TUI_SCREENSHOT_ROWS", 32)
	if err != nil {
		return externalDashboardCaptureConfig{}, false, err
	}
	cols, err := intFromEnv(getenv, "SSHDOCK_TUI_SCREENSHOT_COLS", 120)
	if err != nil {
		return externalDashboardCaptureConfig{}, false, err
	}
	timeout, err := durationFromEnv(getenv, "SSHDOCK_TUI_SCREENSHOT_TIMEOUT", 20*time.Second)
	if err != nil {
		return externalDashboardCaptureConfig{}, false, err
	}
	maxTabs, err := intFromEnv(getenv, "SSHDOCK_TUI_SCREENSHOT_TABS", 8)
	if err != nil {
		return externalDashboardCaptureConfig{}, false, err
	}

	args := []string{"-tt"}
	if port := strings.TrimSpace(getenv("SSHDOCK_TUI_SCREENSHOT_SSH_PORT")); port != "" {
		args = append(args, "-p", port)
	}
	if identity := strings.TrimSpace(getenv("SSHDOCK_TUI_SCREENSHOT_SSH_IDENTITY")); identity != "" {
		args = append(args, "-i", identity, "-o", "IdentitiesOnly=yes")
	}
	args = append(args,
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		target,
	)

	artifactDir := strings.TrimSpace(getenv("SSHDOCK_TUI_SCREENSHOT_DIR"))
	if artifactDir == "" {
		artifactDir = filepath.Join("..", "..", "_artifacts", "tui-screenshots-vps")
	}

	return externalDashboardCaptureConfig{
		Args:        args,
		ArtifactDir: artifactDir,
		Rows:        rows,
		Cols:        cols,
		Timeout:     timeout,
		MaxTabs:     maxTabs,
	}, true, nil
}

func intFromEnv(getenv getenvFunc, key string, fallback int) (int, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func durationFromEnv(getenv getenvFunc, key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration like 30s", key)
	}
	return parsed, nil
}

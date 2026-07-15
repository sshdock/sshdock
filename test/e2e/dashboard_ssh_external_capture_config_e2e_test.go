//go:build e2e

package e2e

import (
	"reflect"
	"testing"
	"time"
)

func TestExternalDashboardCaptureConfigSkipsWithoutTarget(t *testing.T) {
	config, ok, err := externalDashboardCaptureConfigFromEnv(emptyExternalDashboardEnv)
	if err != nil {
		t.Fatalf("externalDashboardCaptureConfigFromEnv: %v", err)
	}
	if ok {
		t.Fatalf("ok = true, want false with config %#v", config)
	}
}

func TestExternalDashboardCaptureConfigBuildsSSHCommand(t *testing.T) {
	env := map[string]string{
		"SSHDOCK_TUI_SCREENSHOT_SSH_TARGET":   "sshdock@sshdock.example.com",
		"SSHDOCK_TUI_SCREENSHOT_SSH_PORT":     "2222",
		"SSHDOCK_TUI_SCREENSHOT_SSH_IDENTITY": "/tmp/dashboard_ed25519",
		"SSHDOCK_TUI_SCREENSHOT_DIR":          "/tmp/sshdock-vps-shots",
		"SSHDOCK_TUI_SCREENSHOT_ROWS":         "40",
		"SSHDOCK_TUI_SCREENSHOT_COLS":         "132",
		"SSHDOCK_TUI_SCREENSHOT_TIMEOUT":      "45s",
		"SSHDOCK_TUI_SCREENSHOT_TABS":         "7",
	}
	config, ok, err := externalDashboardCaptureConfigFromEnv(func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("externalDashboardCaptureConfigFromEnv: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}

	wantArgs := []string{
		"-tt",
		"-p", "2222",
		"-i", "/tmp/dashboard_ed25519",
		"-o", "IdentitiesOnly=yes",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"sshdock@sshdock.example.com",
	}
	if !reflect.DeepEqual(config.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", config.Args, wantArgs)
	}
	if config.ArtifactDir != "/tmp/sshdock-vps-shots" {
		t.Fatalf("artifact dir = %q", config.ArtifactDir)
	}
	if config.Rows != 40 || config.Cols != 132 {
		t.Fatalf("size = %dx%d, want 40x132", config.Rows, config.Cols)
	}
	if config.Timeout != 45*time.Second {
		t.Fatalf("timeout = %s, want 45s", config.Timeout)
	}
	if config.MaxTabs != 7 {
		t.Fatalf("max tabs = %d, want 7", config.MaxTabs)
	}
}

func TestDashboardFrameNameUsesActiveTab(t *testing.T) {
	text := "SSHDock Dashboard | app healthy on local | Overview\n[Overview] Domains Releases Deployments Logs\n"

	if got := activeDashboardTab(text); got != "Overview" {
		t.Fatalf("active tab = %q, want Overview", got)
	}
	if got := dashboardFrameName(activeDashboardTab(text), "initial"); got != "overview" {
		t.Fatalf("frame name = %q, want overview", got)
	}
}

func TestDashboardFrameNamePrefersTitleTabOverContentBrackets(t *testing.T) {
	text := "SSHDock Dashboard | app healthy on local | Logs\nweb-1 | 2026/07/03 [notice] nginx\n"

	if got := activeDashboardTab(text); got != "Logs" {
		t.Fatalf("active tab = %q, want Logs", got)
	}
}

func TestDashboardFrameNameFallsBackToSequence(t *testing.T) {
	if got := dashboardFrameName(activeDashboardTab("SSHDock Dashboard\nno active tab\n"), "tab-1"); got != "tab-1" {
		t.Fatalf("frame name = %q, want tab-1", got)
	}
}

func emptyExternalDashboardEnv(string) string {
	return ""
}

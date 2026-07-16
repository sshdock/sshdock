package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestStoreBackendAppsHealthUsesCurrentWorktreeForServiceStatus(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	failedComposePath := filepath.Join(appsDir, "my-app", "failed", "compose.yml")
	if err := sqlite.CreateRelease(ctx, app.Release{ID: "rel_failed", AppID: "my-app", CommitSHA: "failed", ComposePath: failedComposePath, Status: app.ReleaseStatusFailed, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("CreateRelease failed: %v", err)
	}
	secret := "postgres://secret"
	if err := sqlite.CreateDeployment(ctx, app.Deployment{ID: "dep_failed", AppID: "my-app", ReleaseID: "rel_failed", Status: app.DeploymentStatusFailed, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute), ErrorMessage: "stage=build; detail=image pull failed for " + secret}); err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}
	configService := appconfig.NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), appconfig.WithClock(func() time.Time { return now }))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte(secret)}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	runner := &compose.FakeRunner{Services: []compose.ServiceStatus{{Name: "web", State: "running"}}}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{NodeID: "node-a", AppsDir: appsDir, RecoveryRunner: runner, ConfigManager: configService, Now: func() time.Time { return now }})
	cliRunner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := cliRunner.Run([]string{"apps", "health", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps health exit code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"health: fail", "latest release: rel_failed failed", "latest deploy: dep_failed failed", "last failure: stage=build; detail=image pull failed for <redacted>", "services: 1 running, 0 attention"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("apps health stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), secret) {
		t.Fatalf("apps health leaked config value:\n%s", stdout.String())
	}
	if len(runner.StatusRequests) != 1 {
		t.Fatalf("status requests = %#v", runner.StatusRequests)
	}
	wantComposePath := filepath.Join(appsDir, "my-app", "worktree", "compose.yml")
	if runner.StatusRequests[0].ComposePath != wantComposePath {
		t.Fatalf("status compose path = %q, want %q", runner.StatusRequests[0].ComposePath, wantComposePath)
	}
}

func TestStoreBackendAppsHealthWarnsForRunningServiceWithoutRestartPolicy(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	composePath := filepath.Join(appsDir, "my-app", "worktree", "compose.yml")
	content := "services:\n  web:\n    image: example/web\n  worker:\n    image: example/worker\n    restart: unless-stopped\n"
	if err := os.WriteFile(composePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write Compose file: %v", err)
	}
	if err := sqlite.AttachDomain(ctx, app.Domain{ID: "dom_1", AppID: "my-app", ServiceName: "web", DomainName: "app.example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("attach domain: %v", err)
	}
	runner := &compose.FakeRunner{Services: []compose.ServiceStatus{{Name: "web", State: "running"}, {Name: "worker", State: "running"}}}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{NodeID: "node-a", AppsDir: appsDir, RecoveryRunner: runner, Now: func() time.Time { return now }})

	// When
	report, err := backend.AppHealth("my-app")

	// Then
	if err != nil {
		t.Fatalf("AppHealth: %v", err)
	}
	for _, check := range report.Checks {
		if check.Name == "restart policy" {
			if check.Status != "warn" || !strings.Contains(check.Detail, "web") || strings.Contains(check.Detail, "worker") {
				t.Fatalf("restart policy check = %#v", check)
			}
			return
		}
	}
	t.Fatalf("health checks = %#v, want restart policy warning", report.Checks)
}

func TestStoreBackendAppsHealthReportsMissingCurrentComposeEntry(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	worktreePath := t.TempDir()
	if err := sqlite.CreateApp(ctx, app.App{ID: "new-app", Name: "new-app", WorktreePath: worktreePath, Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{RecoveryRunner: &compose.FakeRunner{}, Now: func() time.Time { return now }})

	// When
	report, err := backend.AppHealth("new-app")

	// Then
	if err != nil {
		t.Fatalf("AppHealth: %v", err)
	}
	for _, check := range report.Checks {
		if check.Name == "services" && check.Status == "warn" && strings.Contains(check.Detail, "compose file not found") {
			return
		}
	}
	t.Fatalf("health checks = %#v, want missing Compose warning", report.Checks)
}

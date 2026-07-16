package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/router"
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
	if err := sqlite.CreateDeployment(ctx, app.Deployment{ID: "dep_failed", AppID: "my-app", ReleaseID: "rel_failed", CommitSHA: "failed-main", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusFailed, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute), ErrorMessage: "stage=build; detail=image pull failed for " + secret}); err != nil {
		t.Fatalf("CreateDeployment failed: %v", err)
	}
	configService := appconfig.NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), appconfig.WithClock(func() time.Time { return now }))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte(secret)}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	runner := &compose.FakeRunner{Services: []compose.ServiceStatus{{Name: "web", State: "running"}}}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:         "node-a",
		AppsDir:        appsDir,
		RecoveryRunner: runner,
		CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) {
			return "failed-main", nil
		}),
		ConfigManager: configService,
		Now:           func() time.Time { return now },
	})
	cliRunner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := cliRunner.Run([]string{"apps", "health", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps health exit code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"health: fail", "current main: failed-main", "latest release: rel_failed failed", "latest deploy: dep_failed failed commit=failed-main trigger=push", "routes: unrouted", "last failure: dep_failed stage=build; detail=image pull failed for <redacted>", "services: 1 running, 0 attention", "service\tweb\trunning"} {
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

func TestStoreBackendAppsHealthReportsCurrentMainAndLatestAttemptIndependently(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	if err := sqlite.CreateDeployment(ctx, app.Deployment{
		ID:         "dep_attempt",
		AppID:      "my-app",
		ReleaseID:  "rel_new",
		CommitSHA:  "attempted-commit",
		Trigger:    app.DeploymentTriggerPush,
		Status:     app.DeploymentStatusSucceeded,
		StartedAt:  now,
		FinishedAt: now,
	}); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		RecoveryRunner: &compose.FakeRunner{Services: []compose.ServiceStatus{{Name: "web", State: "running"}}},
		CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) {
			return "desired-main", nil
		}),
		Now: func() time.Time { return now },
	})

	// When
	report, err := backend.AppHealth("my-app")

	// Then
	if err != nil {
		t.Fatalf("AppHealth: %v", err)
	}
	if report.CurrentMainCommit != "desired-main" {
		t.Fatalf("current main = %q, want desired-main", report.CurrentMainCommit)
	}
	if report.LatestDeploymentID != "dep_attempt" || report.LatestDeploymentCommit != "attempted-commit" || report.LatestDeploymentTrigger != "push" {
		t.Fatalf("latest deployment = %#v", report)
	}
}

func TestStoreBackendAppsHealthReportsActiveRouteState(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	domain := app.Domain{ID: "dom_1", AppID: "my-app", ServiceName: "web", DomainName: "app.example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}
	if err := sqlite.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		Router: &fakeRoutePublisher{StoredRoutes: []router.Route{{
			AppID: domain.AppID, ServiceName: domain.ServiceName, DomainName: domain.DomainName, Port: domain.Port, HTTPS: domain.HTTPS,
		}}},
		Now: func() time.Time { return now },
	})

	// When
	report, err := backend.AppHealth("my-app")

	// Then
	if err != nil {
		t.Fatalf("AppHealth: %v", err)
	}
	if report.RouteStatus != "1 active, 0 attention" || report.ActiveRouteCount != 1 || report.RouteAttentionCount != 0 {
		t.Fatalf("route state = %#v", report)
	}
}

func TestStoreBackendAppsHealthReportsUnavailableMainAndRouteStateWithoutFailing(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 16, 13, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	if err := sqlite.AttachDomain(ctx, app.Domain{ID: "dom_1", AppID: "my-app", ServiceName: "web", DomainName: "app.example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		Router: &fakeRoutePublisher{RoutesErr: errors.New("caddy unavailable")},
		CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) {
			return "", errors.New("main ref missing")
		}),
		Now: func() time.Time { return now },
	})

	// When
	report, err := backend.AppHealth("my-app")

	// Then
	if err != nil {
		t.Fatalf("AppHealth: %v", err)
	}
	if report.CurrentMainCommit != "" || report.RouteStatus != "0 active, 1 attention (unavailable=1)" || report.Health != "warn" {
		t.Fatalf("health report = %#v", report)
	}
	for _, want := range []string{"main ref missing", "push a commit to remote main", "routes"} {
		found := false
		for _, check := range report.Checks {
			if strings.Contains(check.Name+" "+check.Detail, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("checks = %#v, want %q", report.Checks, want)
		}
	}
}

func TestStoreBackendAppsHealthKeepsMostRecentFailureContextAfterSuccess(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 16, 14, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	for _, deployment := range []app.Deployment{
		{ID: "dep_failed", AppID: "my-app", ReleaseID: "rel_new", CommitSHA: "failed", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusFailed, StartedAt: now, FinishedAt: now, FailureStage: "start services", FailureDetail: "container exited", RetryGuidance: "sudo sshdock apps redeploy my-app", ErrorMessage: "container exited"},
		{ID: "dep_succeeded", AppID: "my-app", ReleaseID: "rel_new", CommitSHA: "succeeded", Trigger: app.DeploymentTriggerRedeploy, Status: app.DeploymentStatusSucceeded, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute)},
	} {
		if err := sqlite.CreateDeployment(ctx, deployment); err != nil {
			t.Fatalf("CreateDeployment %s: %v", deployment.ID, err)
		}
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{Now: func() time.Time { return now }})

	// When
	report, err := backend.AppHealth("my-app")

	// Then
	if err != nil {
		t.Fatalf("AppHealth: %v", err)
	}
	if report.LatestDeploymentID != "dep_succeeded" || report.LatestDeploymentStatus != "succeeded" {
		t.Fatalf("latest deployment = %#v", report)
	}
	if report.LastFailureDeploymentID != "dep_failed" || report.LastFailure != "stage=start services; detail=container exited; retry=sudo sshdock apps redeploy my-app" {
		t.Fatalf("recent failure = %q %q", report.LastFailureDeploymentID, report.LastFailure)
	}
}

func TestStoreBackendAppsHealthDoesNotDuplicatePersistedFailureSummary(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 16, 14, 30, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	summary := "stage=validate compose; detail=invalid YAML; changed=deployment failed; fix=fix compose.yml; retry=push a fix"
	if err := sqlite.CreateDeployment(ctx, app.Deployment{ID: "dep_failed", AppID: "my-app", ReleaseID: "rel_new", Status: app.DeploymentStatusFailed, StartedAt: now, FinishedAt: now, FailureStage: "validate compose", FailureDetail: summary, RetryGuidance: "push a fix", ErrorMessage: summary}); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{Now: func() time.Time { return now }})

	// When
	report, err := backend.AppHealth("my-app")

	// Then
	if err != nil {
		t.Fatalf("AppHealth: %v", err)
	}
	if report.LastFailure != summary {
		t.Fatalf("last failure = %q, want %q", report.LastFailure, summary)
	}
}

func TestStoreBackendAppsHealthReportsMissingAndStoppedContainers(t *testing.T) {
	tests := []struct {
		name              string
		services          []compose.ServiceStatus
		wantServiceStatus string
		wantHealth        string
	}{
		{name: "missing containers", wantServiceStatus: "status unavailable", wantHealth: "warn"},
		{name: "stopped container", services: []compose.ServiceStatus{{Name: "web", State: "exited"}}, wantServiceStatus: "0 running, 1 attention", wantHealth: "fail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			ctx := context.Background()
			sqlite := newStoreBackendTestStore(t, ctx)
			appsDir := filepath.Join(t.TempDir(), "apps")
			now := time.Date(2026, 7, 16, 15, 0, 0, 0, time.UTC)
			seedRecoveryApp(t, ctx, sqlite, appsDir, now)
			backend := NewStoreBackend(sqlite, StoreBackendConfig{
				RecoveryRunner: &compose.FakeRunner{Services: tt.services},
				Now:            func() time.Time { return now },
			})

			// When
			report, err := backend.AppHealth("my-app")

			// Then
			if err != nil {
				t.Fatalf("AppHealth: %v", err)
			}
			if report.Health != tt.wantHealth {
				t.Fatalf("health = %q, want %q", report.Health, tt.wantHealth)
			}
			for _, check := range report.Checks {
				if check.Name == "services" && check.Detail == tt.wantServiceStatus {
					return
				}
			}
			t.Fatalf("checks = %#v, want services %q", report.Checks, tt.wantServiceStatus)
		})
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

func TestStoreBackendAppsHealthReportsInvalidCurrentComposeWithoutFailing(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 16, 16, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	composePath := filepath.Join(appsDir, "my-app", "worktree", "compose.yml")
	if err := os.WriteFile(composePath, []byte("services: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile Compose: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		RecoveryRunner: &compose.FakeRunner{Services: []compose.ServiceStatus{{Name: "web", State: "running"}}},
		Now:            func() time.Time { return now },
	})

	// When
	report, err := backend.AppHealth("my-app")

	// Then
	if err != nil {
		t.Fatalf("AppHealth: %v", err)
	}
	for _, check := range report.Checks {
		if check.Name == "restart policy" && check.Status == "warn" && strings.Contains(check.Detail, "invalid YAML") {
			return
		}
	}
	t.Fatalf("checks = %#v, want restart policy warning", report.Checks)
}

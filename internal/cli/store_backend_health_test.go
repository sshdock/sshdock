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
	for _, want := range []string{"health: fail", "current main: failed-main", "attempt release: rel_failed failed", "latest deploy: dep_failed failed commit=failed-main trigger=push", "routes: unrouted", "last failure: dep_failed stage=build; detail=image pull failed for <redacted>", "services: 1 running, 0 attention", "service\tweb\trunning"} {
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

func TestStoreBackendAppsHealthUsesLatestAttemptReleaseAfterGitRollback(t *testing.T) {
	// Given a newer failed release followed by a successful Git push of an older release.
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 16, 11, 30, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	if err := sqlite.UpdateReleaseStatus(ctx, "rel_new", app.ReleaseStatusFailed, now.Add(time.Minute)); err != nil {
		t.Fatalf("UpdateReleaseStatus: %v", err)
	}
	for _, deployment := range []app.Deployment{
		{ID: "dep_failed", AppID: "my-app", ReleaseID: "rel_new", CommitSHA: "new", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusFailed, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute), FailureStage: "start services", FailureDetail: "unhealthy", RetryGuidance: "push a fixed commit"},
		{ID: "dep_rollback", AppID: "my-app", ReleaseID: "rel_old", CommitSHA: "old", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusSucceeded, StartedAt: now.Add(2 * time.Minute), FinishedAt: now.Add(2 * time.Minute)},
	} {
		if err := sqlite.CreateDeployment(ctx, deployment); err != nil {
			t.Fatalf("CreateDeployment(%s): %v", deployment.ID, err)
		}
	}
	domain := app.Domain{ID: "dom_1", AppID: "my-app", ServiceName: "web", DomainName: "app.example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}
	if err := sqlite.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		RecoveryRunner: &compose.FakeRunner{Services: []compose.ServiceStatus{{Name: "web", State: "running"}}},
		Router: &fakeRoutePublisher{StoredRoutes: []router.Route{{
			AppID: domain.AppID, ServiceName: domain.ServiceName, DomainName: domain.DomainName, Port: domain.Port, HTTPS: domain.HTTPS,
		}}},
		CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) {
			return "old", nil
		}),
		Now: func() time.Time { return now },
	})

	// When current health is calculated after the rollback deployment succeeds.
	report, err := backend.AppHealth("my-app")

	// Then the current attempt's release is healthy and the failed forward attempt stays visible separately.
	if err != nil {
		t.Fatalf("AppHealth: %v", err)
	}
	if report.Health != "ok" || report.AttemptReleaseID != "rel_old" || report.AttemptReleaseStatus != app.ReleaseStatusSucceeded {
		t.Fatalf("health release = %#v, want rel_old succeeded and health ok", report)
	}
	if report.LatestDeploymentID != "dep_rollback" || report.LastFailureDeploymentID != "dep_failed" {
		t.Fatalf("deployment history = %#v", report)
	}
	if report.LastSuccessfulDeploymentID != "dep_rollback" || report.LastSuccessfulDeploymentCommit != "old" || report.LastSuccessfulReleaseID != "rel_old" {
		t.Fatalf("last successful deployment = %#v", report)
	}
}

func TestStoreBackendAppsHealthDoesNotBorrowHistoricalReleaseForPendingAttempt(t *testing.T) {
	for _, releaseID := range []string{"rel_pending", ""} {
		t.Run("release="+releaseID, func(t *testing.T) {
			ctx := context.Background()
			sqlite := newStoreBackendTestStore(t, ctx)
			now := time.Date(2026, 10, 23, 11, 0, 0, 0, time.UTC)
			seedRecoveryApp(t, ctx, sqlite, filepath.Join(t.TempDir(), "apps"), now)
			for _, deployment := range []app.Deployment{
				{ID: "dep_ok", AppID: "my-app", ReleaseID: "rel_new", CommitSHA: "new", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusSucceeded, StartedAt: now},
				{ID: "dep_pending", AppID: "my-app", ReleaseID: releaseID, CommitSHA: "unbuilt", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now.Add(time.Minute)},
			} {
				if err := sqlite.CreateDeployment(ctx, deployment); err != nil {
					t.Fatal(err)
				}
			}
			report, err := NewStoreBackend(sqlite, StoreBackendConfig{}).AppHealth("my-app")
			if err != nil {
				t.Fatal(err)
			}
			if report.AttemptReleaseID != releaseID || report.AttemptReleaseStatus != "" || report.LatestDeploymentID != "dep_pending" || report.LastSuccessfulReleaseID != "rel_new" {
				t.Fatalf("pending attempt borrowed historical release: %#v", report)
			}
			for _, check := range report.Checks {
				if check.Name == "release" && check.Status == "warn" && strings.Contains(check.Detail, "no release record for latest deployment dep_pending") {
					return
				}
			}
			t.Fatalf("missing release-record warning: %#v", report.Checks)
		})
	}
}

func TestStoreBackendAppsHealthKeepsSuccessfulIdentityIndependentOfLatestAttempt(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	attempt := func(id, commit, release string, status app.DeploymentStatus, offset time.Duration) app.Deployment {
		return app.Deployment{ID: id, AppID: "my-app", ReleaseID: release, CommitSHA: commit, Trigger: app.DeploymentTriggerPush, Status: status, StartedAt: now.Add(offset)}
	}
	interrupted := attempt("dep_interrupted", "new", "rel_new", app.DeploymentStatusFailed, time.Minute)
	interrupted.FailureStage = "daemon restart"
	interrupted.FailureDetail = "daemon restarted while this deployment was active; it was not automatically rerun"
	for _, test := range []struct {
		name, main, latest, success, successCommit, successRelease, failure string
		attempts                                                            []app.Deployment
		stale                                                               bool
	}{
		{
			name: "failed same commit retry", main: "new", latest: "dep_retry", success: "dep_ok", successCommit: "new", successRelease: "rel_new", failure: "dep_retry",
			attempts: []app.Deployment{attempt("dep_ok", "new", "rel_new", app.DeploymentStatusSucceeded, 0), attempt("dep_retry", "new", "rel_new", app.DeploymentStatusFailed, time.Minute)},
		},
		{
			name: "queued attempt", main: "new", latest: "dep_pending", success: "dep_ok", successCommit: "old", successRelease: "rel_old",
			attempts: []app.Deployment{attempt("dep_ok", "old", "rel_old", app.DeploymentStatusSucceeded, 0), attempt("dep_pending", "new", "rel_new", app.DeploymentStatusPending, time.Minute)},
		},
		{
			name: "deploying attempt", main: "new", latest: "dep_deploying", success: "dep_ok", successCommit: "old", successRelease: "rel_old",
			attempts: []app.Deployment{attempt("dep_ok", "old", "rel_old", app.DeploymentStatusSucceeded, 0), attempt("dep_deploying", "new", "rel_new", app.DeploymentStatusDeploying, time.Minute)},
		},
		{
			name: "interrupted attempt", main: "new", latest: "dep_interrupted", success: "dep_ok", successCommit: "old", successRelease: "rel_old", failure: "dep_interrupted",
			attempts: []app.Deployment{attempt("dep_ok", "old", "rel_old", app.DeploymentStatusSucceeded, 0), interrupted},
		},
		{
			name: "stale current main", main: "new", latest: "dep_ok", success: "dep_ok", successCommit: "old", successRelease: "rel_old", stale: true,
			attempts: []app.Deployment{attempt("dep_ok", "old", "rel_old", app.DeploymentStatusSucceeded, 0)},
		},
		{name: "current main without attempts", main: "new", stale: true},
		{
			name: "failed attempt without success", main: "new", latest: "dep_failed", failure: "dep_failed",
			attempts: []app.Deployment{attempt("dep_failed", "new", "rel_new", app.DeploymentStatusFailed, 0)},
		},
		{
			name: "tied attempts use ID", main: "old", latest: "dep_z", success: "dep_z", successCommit: "old", successRelease: "rel_old",
			attempts: []app.Deployment{attempt("dep_z", "old", "rel_old", app.DeploymentStatusSucceeded, 0), attempt("dep_a", "new", "rel_new", app.DeploymentStatusSucceeded, 0)},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			sqlite := newStoreBackendTestStore(t, ctx)
			seedRecoveryApp(t, ctx, sqlite, filepath.Join(t.TempDir(), "apps"), now)
			for _, deployment := range test.attempts {
				if err := sqlite.CreateDeployment(ctx, deployment); err != nil {
					t.Fatal(err)
				}
			}
			domain := app.Domain{ID: "dom_1", AppID: "my-app", ServiceName: "web", DomainName: "app.example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}
			if err := sqlite.AttachDomain(ctx, domain); err != nil {
				t.Fatal(err)
			}
			backend := NewStoreBackend(sqlite, StoreBackendConfig{
				RecoveryRunner: &compose.FakeRunner{Services: []compose.ServiceStatus{{Name: "web", State: "running"}}},
				Router:         &fakeRoutePublisher{StoredRoutes: []router.Route{{AppID: domain.AppID, ServiceName: domain.ServiceName, DomainName: domain.DomainName, Port: domain.Port, HTTPS: domain.HTTPS}}},
				CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) {
					return test.main, nil
				}),
			})
			report, err := backend.AppHealth("my-app")
			if err != nil {
				t.Fatal(err)
			}
			if report.LatestDeploymentID != test.latest || report.LastSuccessfulDeploymentID != test.success || report.LastSuccessfulDeploymentCommit != test.successCommit || report.LastSuccessfulReleaseID != test.successRelease || report.LastFailureDeploymentID != test.failure {
				t.Fatalf("deployment identities = %#v", report)
			}
			stale := false
			for _, check := range report.Checks {
				if check.Name == "current main deployment" {
					stale = check.Status == "warn" && strings.Contains(check.Detail, test.main) && strings.Contains(check.Detail, "sudo sshdock apps redeploy my-app")
				}
			}
			if stale != test.stale {
				t.Fatalf("stale current main warning = %v, want %v: %#v", stale, test.stale, report.Checks)
			}
			if report.ActiveRouteCount != 1 || report.RouteAttentionCount != 0 || report.RunningServiceCount != 1 || report.AttentionServiceCount != 0 {
				t.Fatalf("attempt identity changed independent runtime/route observations: %#v", report)
			}
		})
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

func TestStoreBackendRestartPolicyPrefersNativeBeforeLocalFallback(t *testing.T) {
	for _, test := range []struct {
		name, fallback, wantStatus, wantDetail string
		native                                 []string
		nativeErr                              error
	}{
		{name: "native empty result", wantStatus: "ok", wantDetail: "configured for routed and running services"},
		{name: "native warning", native: []string{"web"}, wantStatus: "warn", wantDetail: "default non-restarting policy: web"},
		{name: "local becomes readable during native failure", nativeErr: errors.New("native unavailable"), fallback: "services:\n  web:\n    restart: always\n  worker:\n    restart: unless-stopped\n", wantStatus: "ok", wantDetail: "configured for routed and running services"},
		{name: "both unavailable", nativeErr: errors.New("native unavailable"), wantStatus: "warn", wantDetail: "inspection unavailable: load Compose restart policies:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "compose.yml")
			calls := 0
			runner := &healthPolicyRunner{
				FakeRunner: &compose.FakeRunner{Services: []compose.ServiceStatus{
					{Name: "web", State: "running"}, {Name: "worker", State: "exited"}, {Name: "ignored", State: "exited"},
				}},
				inspect: func(request compose.StatusRequest, candidates []string) ([]string, error) {
					calls++
					if request.AppName != "my-app" || request.ProjectDir != dir || request.ComposePath != path || strings.Join(candidates, ",") != "worker,web" {
						t.Fatalf("inspection = %#v, candidates = %v", request, candidates)
					}
					if test.fallback != "" {
						if err := os.WriteFile(path, []byte(test.fallback), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					return test.native, test.nativeErr
				},
			}
			backend := NewStoreBackend(nil, StoreBackendConfig{RecoveryRunner: runner})
			var report AppHealth
			err := backend.addServiceStatus(context.Background(), app.App{ID: "my-app"}, dir, path, []app.Domain{{ServiceName: "worker"}}, &report)
			if err != nil || calls != 1 {
				t.Fatalf("service status error = %v, inspections = %d", err, calls)
			}
			for _, check := range report.Checks {
				if check.Name == "restart policy" {
					if check.Status != test.wantStatus || !strings.Contains(check.Detail, test.wantDetail) {
						t.Fatalf("restart policy = %#v, want %s %q", check, test.wantStatus, test.wantDetail)
					}
					return
				}
			}
			t.Fatalf("missing restart policy check: %#v", report.Checks)
		})
	}
}

type healthPolicyRunner struct {
	*compose.FakeRunner
	inspect func(compose.StatusRequest, []string) ([]string, error)
}

func (r *healthPolicyRunner) NonRestartingServices(_ context.Context, request compose.StatusRequest, candidates []string) ([]string, error) {
	return r.inspect(request, candidates)
}

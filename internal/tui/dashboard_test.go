package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestDashboardHandlerRendersAppsDetailsStatusDomainsHistoryAndLogs(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	worktreePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile Compose: %v", err)
	}
	store := &fakeDashboardStore{
		apps: []app.App{
			{
				ID:           "my-app",
				Name:         "my-app",
				NodeID:       "local",
				WorktreePath: worktreePath,
				Status:       app.AppStatusHealthy,
			},
		},
		releasesByApp: map[string][]app.Release{
			"my-app": {
				{ID: "rel_old", AppID: "my-app", CommitSHA: "old", ComposePath: "/historical/old/compose.yml", Status: app.ReleaseStatusFailed, CreatedAt: now.Add(-time.Hour)},
				{ID: "rel_new", AppID: "my-app", CommitSHA: "abc123", ComposePath: "/historical/new/compose.yml", Status: app.ReleaseStatusSucceeded, CreatedAt: now},
			},
		},
		domainsByApp: map[string][]app.Domain{
			"my-app": {
				{ID: "dom_1", AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true},
			},
		},
		deploymentsByApp: map[string][]app.Deployment{
			"my-app": {
				{ID: "dep_1", AppID: "my-app", ReleaseID: "rel_new", Status: app.DeploymentStatusSucceeded, StartedAt: now, FinishedAt: now},
				{ID: "dep_2", AppID: "my-app", ReleaseID: "rel_old", CommitSHA: "old", Trigger: app.DeploymentTriggerRedeploy, Status: app.DeploymentStatusFailed, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(2 * time.Minute), FailureStage: "build services", FailureDetail: "build services failed: docker output included postgres://secret and legacy-secret", RetryGuidance: "sudo sshdock apps redeploy my-app", ErrorMessage: "stage=build services; detail=build services failed: docker output included postgres://secret and legacy-secret"},
			},
		},
		eventsByApp: map[string][]app.Event{
			"my-app": {
				{ID: "evt_1", AppID: "my-app", Type: "deploy.succeeded", Message: "Deploy succeeded", CreatedAt: now},
			},
		},
	}
	runner := &compose.FakeRunner{
		Services:  []compose.ServiceStatus{{Name: "web", State: "running"}},
		LogOutput: "first log\npostgres://secret\nlegacy-secret\n",
	}
	var output bytes.Buffer
	config := &fakeDashboardConfigResolver{
		env:             map[string]string{"DATABASE_URL": "postgres://secret"},
		redactionValues: map[string]string{"my-app/DATABASE_URL": "postgres://secret", "my-app/worker/API_TOKEN": "legacy-secret"},
	}
	health := &fakeDashboardHealthProvider{reports: map[string]app.HealthReport{
		"my-app": {
			Health:                  "fail",
			RouteStatus:             "routed",
			LatestDeploymentStatus:  app.DeploymentStatusFailed,
			ServiceCount:            1,
			RunningServiceCount:     1,
			Services:                []app.ServiceHealth{{Name: "web", State: "running"}},
			LastFailureDeploymentID: "",
			LastFailure:             "stage=build services; detail=build services failed: docker output included <redacted>",
		},
	}}
	handler := NewDashboardHandlerWithConfig(store, runner, config, health)

	if err := handler.Render(ctx, &output); err != nil {
		t.Fatalf("Render: %v", err)
	}

	rendered := output.String()
	for _, want := range []string{
		"SSHDock Dashboard",
		"Apps",
		"my-app",
		"healthy",
		"latest=succeeded",
		"domains=1",
		"App my-app",
		"Route: routed",
		"Latest deploy: failed",
		"Service status: 1 running",
		"Last failure: stage=build services; detail=build services failed: docker output included <redacted>",
		"Services",
		"web running",
		"Domains",
		"example.com -> web:3000",
		"Releases",
		"rel_new succeeded abc123",
		"Deployments",
		"dep_1 succeeded rel_new",
		"dep_2 failed rel_old",
		"trigger=redeploy commit=old",
		"failure-stage=build services",
		"retry=sudo sshdock apps redeploy my-app",
		"stage=build services; detail=build services failed: docker output included <redacted>",
		"Events",
		"deploy.succeeded Deploy succeeded",
		"Logs web",
		"first log",
		"<redacted>",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("dashboard output missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "postgres://secret") || strings.Contains(rendered, "legacy-secret") {
		t.Fatalf("dashboard output leaked config value:\n%s", rendered)
	}
	if len(runner.StatusRequests) != 0 {
		t.Fatalf("dashboard repeated shared health status request: %#v", runner.StatusRequests)
	}
	if len(runner.LogsRequests) != 1 {
		t.Fatalf("logs requests = %#v", runner.LogsRequests)
	}
	logsRequest := runner.LogsRequests[0]
	if logsRequest.AppName != "my-app" || logsRequest.ServiceName != "web" || logsRequest.Lines != 50 {
		t.Fatalf("logs request = %#v", logsRequest)
	}
	if logsRequest.Env["DATABASE_URL"] != "postgres://secret" {
		t.Fatalf("logs request env = %#v", logsRequest.Env)
	}
}

func TestDashboardHandlerBuildsReusableSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	worktreePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile Compose: %v", err)
	}
	store := &fakeDashboardStore{
		apps: []app.App{
			{ID: "my-app", Name: "my-app", NodeID: "local", WorktreePath: worktreePath, Status: app.AppStatusHealthy},
		},
		releasesByApp: map[string][]app.Release{
			"my-app": {
				{ID: "rel_new", AppID: "my-app", CommitSHA: "abc123", ComposePath: "/historical/new/compose.yml", Status: app.ReleaseStatusSucceeded, CreatedAt: now},
			},
		},
		domainsByApp: map[string][]app.Domain{
			"my-app": {
				{ID: "dom_1", AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true},
			},
		},
		deploymentsByApp: map[string][]app.Deployment{
			"my-app": {
				{ID: "dep_1", AppID: "my-app", ReleaseID: "rel_new", Status: app.DeploymentStatusSucceeded, StartedAt: now, FinishedAt: now},
			},
		},
		eventsByApp: map[string][]app.Event{
			"my-app": {
				{ID: "evt_1", AppID: "my-app", Type: "deploy.succeeded", Message: "Deploy succeeded", CreatedAt: now},
			},
		},
	}
	runner := &compose.FakeRunner{
		Services:  []compose.ServiceStatus{{Name: "web", State: "running"}},
		LogOutput: "first log\nsecond log\n",
	}
	health := &fakeDashboardHealthProvider{reports: map[string]app.HealthReport{
		"my-app": {
			Health:                 "ok",
			RouteStatus:            "routed",
			LatestDeploymentStatus: app.DeploymentStatusSucceeded,
			ServiceCount:           1,
			RunningServiceCount:    1,
			Services:               []app.ServiceHealth{{Name: "web", State: "running"}},
		},
	}}
	handler := NewDashboardHandler(store, runner, health)

	snapshot, err := handler.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if len(snapshot.Apps.Rows()) != 1 {
		t.Fatalf("snapshot apps = %#v", snapshot.Apps.Rows())
	}
	if len(snapshot.AppsByID["my-app"].Detail.Services()) != 1 {
		t.Fatalf("snapshot detail services = %#v", snapshot.AppsByID["my-app"].Detail.Services())
	}
	if got := snapshot.AppsByID["my-app"].Logs["web"].Lines; len(got) != 2 || got[0] != "first log" || got[1] != "second log" {
		t.Fatalf("snapshot logs = %#v", got)
	}
	if got := snapshot.AppsByID["my-app"].Detail.Events(); len(got) != 1 || got[0].Type != "deploy.succeeded" {
		t.Fatalf("snapshot events = %#v", got)
	}
}

func TestDashboardHandlerRendersSharedHealthReport(t *testing.T) {
	// Given
	worktreePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile Compose: %v", err)
	}
	store := &fakeDashboardStore{apps: []app.App{{ID: "my-app", Name: "my-app", NodeID: "local", WorktreePath: worktreePath, Status: app.AppStatusHealthy}}}
	health := &fakeDashboardHealthProvider{reports: map[string]app.HealthReport{
		"my-app": {
			Health:                  "fail",
			CurrentMainCommit:       "desired-main",
			LatestDeploymentID:      "dep_failed",
			LatestDeploymentCommit:  "desired-main",
			LatestDeploymentTrigger: app.DeploymentTriggerPush,
			LatestDeploymentStatus:  "failed",
			RouteStatus:             "0 active, 1 attention",
			ServiceCount:            2,
			RunningServiceCount:     1,
			AttentionServiceCount:   1,
			Services:                []app.ServiceHealth{{Name: "web", State: "running"}, {Name: "worker", State: "exited"}},
			LastFailureDeploymentID: "dep_failed",
			LastFailure:             "stage=start; detail=container exited",
			Checks: []app.HealthCheck{{
				Status: "warn", Name: "routes", Detail: "0 active, 1 attention (unavailable=1)",
			}},
		},
	}}
	runner := &compose.FakeRunner{Services: []compose.ServiceStatus{{Name: "stale", State: "running"}}, LogOutput: "log\n"}
	handler := NewDashboardHandler(store, runner, health)
	var output bytes.Buffer

	// When
	err := handler.Render(context.Background(), &output)

	// Then
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"Health: fail",
		"Current main: desired-main",
		"Route: 0 active, 1 attention",
		"Latest deploy: dep_failed failed commit=desired-main trigger=push",
		"Service status: 1 running, 1 attention",
		"Last failure: dep_failed stage=start; detail=container exited",
		"Health checks",
		"warn routes: 0 active, 1 attention (unavailable=1)",
		"web running",
		"worker exited",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("dashboard output missing %q:\n%s", want, output.String())
		}
	}
	if len(runner.StatusRequests) != 0 {
		t.Fatalf("dashboard repeated Compose status instead of using shared report: %#v", runner.StatusRequests)
	}
}

func TestDashboardHandlerRendersEmptyAppList(t *testing.T) {
	ctx := context.Background()
	var output bytes.Buffer
	handler := NewDashboardHandler(&fakeDashboardStore{}, &compose.FakeRunner{}, &fakeDashboardHealthProvider{})

	if err := handler.Render(ctx, &output); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(output.String(), "No apps") {
		t.Fatalf("dashboard output = %q", output.String())
	}
}

type fakeDashboardStore struct {
	apps             []app.App
	releasesByApp    map[string][]app.Release
	domainsByApp     map[string][]app.Domain
	deploymentsByApp map[string][]app.Deployment
	eventsByApp      map[string][]app.Event
}

type fakeDashboardConfigResolver struct {
	env             map[string]string
	redactionValues map[string]string
}

type fakeDashboardHealthProvider struct {
	reports map[string]app.HealthReport
}

func (f *fakeDashboardHealthProvider) AppHealth(appName string) (app.HealthReport, error) {
	return f.reports[appName], nil
}

func (f *fakeDashboardConfigResolver) ResolveAppConfig(_ context.Context, _ string) (map[string]string, error) {
	result := make(map[string]string, len(f.env))
	for key, value := range f.env {
		result[key] = value
	}
	return result, nil
}

func (f *fakeDashboardConfigResolver) RedactionValues(_ context.Context, _ string) (map[string]string, error) {
	result := make(map[string]string, len(f.redactionValues))
	for key, value := range f.redactionValues {
		result[key] = value
	}
	return result, nil
}

func (f *fakeDashboardStore) ListApps(context.Context) ([]app.App, error) {
	return append([]app.App(nil), f.apps...), nil
}

func (f *fakeDashboardStore) ListReleasesByApp(_ context.Context, appID string) ([]app.Release, error) {
	return append([]app.Release(nil), f.releasesByApp[appID]...), nil
}

func (f *fakeDashboardStore) ListDomainsByApp(_ context.Context, appID string) ([]app.Domain, error) {
	return append([]app.Domain(nil), f.domainsByApp[appID]...), nil
}

func (f *fakeDashboardStore) ListDeploymentsByApp(_ context.Context, appID string) ([]app.Deployment, error) {
	return append([]app.Deployment(nil), f.deploymentsByApp[appID]...), nil
}

func (f *fakeDashboardStore) ListEventsByApp(_ context.Context, appID string) ([]app.Event, error) {
	return append([]app.Event(nil), f.eventsByApp[appID]...), nil
}

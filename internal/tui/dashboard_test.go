package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
	"github.com/iketiunn/rumbase/internal/compose"
)

func TestDashboardHandlerRendersAppsDetailsStatusDomainsHistoryAndLogs(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	store := &fakeDashboardStore{
		apps: []app.App{
			{
				ID:           "my-app",
				Name:         "my-app",
				NodeID:       "local",
				WorktreePath: "/tmp/apps/my-app/worktree",
				Status:       app.AppStatusHealthy,
			},
		},
		releasesByApp: map[string][]app.Release{
			"my-app": {
				{ID: "rel_old", AppID: "my-app", CommitSHA: "old", ComposePath: "/tmp/apps/my-app/worktree/compose.yml", Status: app.ReleaseStatusFailed, CreatedAt: now.Add(-time.Hour)},
				{ID: "rel_new", AppID: "my-app", CommitSHA: "abc123", ComposePath: "/tmp/apps/my-app/worktree/compose.yml", Status: app.ReleaseStatusSucceeded, CreatedAt: now},
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
	}
	runner := &compose.FakeRunner{
		Services:  []compose.ServiceStatus{{Name: "web", State: "running"}},
		LogOutput: "first log\nsecond log\n",
	}
	var output bytes.Buffer
	handler := NewDashboardHandler(store, runner)

	if err := handler.Render(ctx, &output); err != nil {
		t.Fatalf("Render: %v", err)
	}

	rendered := output.String()
	for _, want := range []string{
		"Rhumbase Dashboard",
		"Apps",
		"my-app",
		"healthy",
		"latest=succeeded",
		"domains=1",
		"App my-app",
		"Services",
		"web running",
		"Domains",
		"example.com -> web:3000",
		"Releases",
		"rel_new succeeded abc123",
		"Deployments",
		"dep_1 succeeded rel_new",
		"Logs web",
		"first log",
		"second log",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("dashboard output missing %q:\n%s", want, rendered)
		}
	}
	if len(runner.StatusRequests) != 1 {
		t.Fatalf("status requests = %#v", runner.StatusRequests)
	}
	statusRequest := runner.StatusRequests[0]
	if statusRequest.AppName != "my-app" || statusRequest.ComposePath != "/tmp/apps/my-app/worktree/compose.yml" || statusRequest.ProjectDir != "/tmp/apps/my-app/worktree" {
		t.Fatalf("status request = %#v", statusRequest)
	}
	if len(runner.LogsRequests) != 1 {
		t.Fatalf("logs requests = %#v", runner.LogsRequests)
	}
	logsRequest := runner.LogsRequests[0]
	if logsRequest.AppName != "my-app" || logsRequest.ServiceName != "web" || logsRequest.Lines != 50 {
		t.Fatalf("logs request = %#v", logsRequest)
	}
}

func TestDashboardHandlerBuildsReusableSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	store := &fakeDashboardStore{
		apps: []app.App{
			{ID: "my-app", Name: "my-app", NodeID: "local", Status: app.AppStatusHealthy},
		},
		releasesByApp: map[string][]app.Release{
			"my-app": {
				{ID: "rel_new", AppID: "my-app", CommitSHA: "abc123", ComposePath: "/tmp/apps/my-app/worktree/compose.yml", Status: app.ReleaseStatusSucceeded, CreatedAt: now},
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
	}
	runner := &compose.FakeRunner{
		Services:  []compose.ServiceStatus{{Name: "web", State: "running"}},
		LogOutput: "first log\nsecond log\n",
	}
	handler := NewDashboardHandler(store, runner)

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
}

func TestDashboardHandlerRendersEmptyAppList(t *testing.T) {
	ctx := context.Background()
	var output bytes.Buffer
	handler := NewDashboardHandler(&fakeDashboardStore{}, &compose.FakeRunner{})

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

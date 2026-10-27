package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestNewAppListView(t *testing.T) {
	apps := []app.App{
		{ID: "app_1", Name: "my-app", NodeID: "local", Status: app.AppStatusHealthy},
	}
	latest := map[string]app.DeploymentStatus{
		"app_1": app.DeploymentStatusSucceeded,
	}
	domains := map[string][]app.Domain{
		"app_1": {
			{ID: "dom_1", AppID: "app_1", DomainName: "example.com"},
			{ID: "dom_2", AppID: "app_1", DomainName: "www.example.com"},
		},
	}

	view := NewAppListView(apps, latest, domains)

	if len(view.Items) != 1 {
		t.Fatalf("items = %#v", view.Items)
	}
	item := view.Items[0]
	if item.Name != "my-app" || item.Status != "healthy" || item.NodeID != "local" {
		t.Fatalf("item = %#v", item)
	}
	if item.LatestDeploymentStatus != "succeeded" {
		t.Fatalf("LatestDeploymentStatus = %q", item.LatestDeploymentStatus)
	}
	if item.DomainCount != 2 {
		t.Fatalf("DomainCount = %d", item.DomainCount)
	}
}

func TestNewAppDetailView(t *testing.T) {
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	model := app.App{ID: "app_1", Name: "my-app", NodeID: "local", Status: app.AppStatusHealthy}
	report := app.HealthReport{
		Services:               []app.ServiceHealth{{Name: "web", State: "running"}},
		RouteStatus:            "1 active, 0 attention",
		LatestDeploymentStatus: app.DeploymentStatusSucceeded,
		ServiceCount:           1,
		RunningServiceCount:    1,
	}
	domains := []app.Domain{{ID: "dom_1", AppID: "app_1", ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true}}
	releases := []app.Release{{ID: "rel_1", AppID: "app_1", CommitSHA: "abc123", ComposePath: "compose.yml", Status: app.ReleaseStatusSucceeded, CreatedAt: now}}
	deployments := []app.Deployment{{ID: "dep_1", AppID: "app_1", ReleaseID: "rel_1", Status: app.DeploymentStatusSucceeded, StartedAt: now, FinishedAt: now}}
	events := []app.Event{{ID: "evt_1", AppID: "app_1", Type: "deploy.succeeded", Message: "Deploy succeeded", CreatedAt: now}}

	view := NewAppDetailView(model, report, domains, releases, deployments, events)

	if view.App.Name != "my-app" || view.App.Status != "healthy" {
		t.Fatalf("app view = %#v", view.App)
	}
	if len(view.Services) != 1 || view.Services[0].Name != "web" || view.Services[0].State != "running" {
		t.Fatalf("services = %#v", view.Services)
	}
	if len(view.Domains) != 1 || view.Domains[0].DomainName != "example.com" || view.Domains[0].Target != "web:3000" {
		t.Fatalf("domains = %#v", view.Domains)
	}
	if len(view.Releases) != 1 || view.Releases[0].CommitSHA != "abc123" || view.Releases[0].Status != "succeeded" {
		t.Fatalf("releases = %#v", view.Releases)
	}
	if len(view.Deployments) != 1 || view.Deployments[0].Status != "succeeded" {
		t.Fatalf("deployments = %#v", view.Deployments)
	}
	if len(view.Events) != 1 || view.Events[0].Type != "deploy.succeeded" || view.Events[0].Message != "Deploy succeeded" {
		t.Fatalf("events = %#v", view.Events)
	}
	if view.Health.RouteStatus != report.RouteStatus || view.Health.LatestDeploymentStatus != "succeeded" || view.Health.ServiceStatus != "1 running" {
		t.Fatalf("health = %#v", view.Health)
	}
}

func TestAppDetailViewLatestDeployments(t *testing.T) {
	deployments := []DeploymentView{{ID: "dep_1"}, {ID: "dep_2"}, {ID: "dep_3"}}
	for _, tt := range []struct {
		name  string
		view  AppDetailView
		limit int
		want  []DeploymentView
	}{
		{name: "empty", view: AppDetailView{}, limit: 2},
		{name: "zero means all", view: AppDetailView{Deployments: deployments}, limit: 0, want: deployments},
		{name: "negative means all", view: AppDetailView{Deployments: deployments}, limit: -1, want: deployments},
		{name: "exact count", view: AppDetailView{Deployments: deployments}, limit: 3, want: deployments},
		{name: "larger than count", view: AppDetailView{Deployments: deployments}, limit: 4, want: deployments},
		{name: "latest in original order", view: AppDetailView{Deployments: deployments}, limit: 2, want: deployments[1:]},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.view.LatestDeployments(tt.limit); !slices.Equal(got, tt.want) {
				t.Fatalf("LatestDeployments(%d) = %#v, want %#v", tt.limit, got, tt.want)
			}
		})
	}
}

func TestNewAppDetailViewUsesSharedHealthReportSemantics(t *testing.T) {
	// Given
	report := app.HealthReport{
		Health:                  "fail",
		CurrentMainCommit:       "desired-main",
		LatestDeploymentID:      "dep_failed",
		LatestDeploymentCommit:  "desired-main",
		LatestDeploymentTrigger: app.DeploymentTriggerRedeploy,
		LatestDeploymentStatus:  "failed",
		RouteStatus:             "1 active, 0 attention",
		ServiceCount:            2,
		RunningServiceCount:     1,
		AttentionServiceCount:   1,
		LastFailureDeploymentID: "dep_failed",
		LastFailure:             "stage=start; detail=container exited",
		Services:                []app.ServiceHealth{{Name: "web", State: "running"}, {Name: "worker", State: "exited"}},
		Checks:                  []app.HealthCheck{{Name: "deployment", Status: "fail", Detail: "latest attempt failed"}},
	}
	history := []app.Deployment{{ID: "dep_stale_success", Status: app.DeploymentStatusSucceeded}}

	// When
	view := NewAppDetailView(app.App{ID: "app_1"}, report, nil, nil, history, nil)

	// Then
	if view.Health.OverallStatus != "fail" || view.Health.CurrentMainCommit != "desired-main" {
		t.Fatalf("health = %#v", view.Health)
	}
	if view.Health.LatestDeploymentID != "dep_failed" || view.Health.LatestDeploymentCommit != "desired-main" || view.Health.LatestDeploymentStatus != "failed" || view.Health.LatestDeploymentTrigger != "redeploy" {
		t.Fatalf("latest deployment health = %#v", view.Health)
	}
	if view.Health.RouteStatus != "1 active, 0 attention" || view.Health.ServiceStatus != "1 running, 1 attention" {
		t.Fatalf("runtime health = %#v", view.Health)
	}
	if view.Health.LastFailureDeploymentID != "dep_failed" || view.Health.LastFailure != "stage=start; detail=container exited" {
		t.Fatalf("failure health = %#v", view.Health)
	}
	if !slices.Equal(view.Services, []ServiceView{{Name: "web", State: "running"}, {Name: "worker", State: "exited"}}) {
		t.Fatalf("services = %#v", view.Services)
	}
	if len(view.Deployments) != 1 || view.Deployments[0].ID != "dep_stale_success" || view.Deployments[0].Status != "succeeded" {
		t.Fatalf("history = %#v", view.Deployments)
	}
	if !slices.Equal(view.Health.Checks, report.Checks) {
		t.Fatalf("checks = %#v", view.Health.Checks)
	}
}

func TestNewAppDetailViewExposesDeploymentAttemptHistory(t *testing.T) {
	// Given
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	deployment := app.Deployment{
		ID:            "dep_attempt",
		AppID:         "app_1",
		ReleaseID:     "rel_app_1_abc123",
		CommitSHA:     "abc123",
		Trigger:       app.DeploymentTriggerRedeploy,
		Status:        app.DeploymentStatusFailed,
		StartedAt:     now,
		FinishedAt:    now.Add(time.Minute),
		FailureStage:  "start services",
		FailureDetail: "container exited",
		RetryGuidance: "sudo sshdock apps redeploy app_1",
		ErrorMessage:  "container exited",
	}

	// When
	view := NewAppDetailView(app.App{ID: "app_1"}, app.HealthReport{}, nil, nil, []app.Deployment{deployment}, nil)

	// Then
	if len(view.Deployments) != 1 {
		t.Fatalf("deployments = %#v", view.Deployments)
	}
	got := view.Deployments[0]
	if got.CommitSHA != deployment.CommitSHA || got.Trigger != string(deployment.Trigger) || got.FailureStage != deployment.FailureStage || got.FailureDetail != deployment.FailureDetail || got.RetryGuidance != deployment.RetryGuidance {
		t.Fatalf("deployment view = %#v", got)
	}
}

func TestNewLogsView(t *testing.T) {
	view := NewLogsView("first\nsecond\n")
	if strings.Join(view.Lines, ",") != "first,second" {
		t.Fatalf("Lines = %#v", view.Lines)
	}
}

package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestNewAppListView(t *testing.T) {
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	apps := []app.App{
		{ID: "app_1", Name: "my-app", NodeID: "local", Status: app.AppStatusHealthy},
	}
	latest := map[string]app.Release{
		"app_1": {ID: "rel_1", AppID: "app_1", Status: app.ReleaseStatusSucceeded, CreatedAt: now},
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
	if item.LatestReleaseStatus != "succeeded" {
		t.Fatalf("LatestReleaseStatus = %q", item.LatestReleaseStatus)
	}
	if item.DomainCount != 2 {
		t.Fatalf("DomainCount = %d", item.DomainCount)
	}
}

func TestNewAppDetailView(t *testing.T) {
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	model := app.App{ID: "app_1", Name: "my-app", NodeID: "local", Status: app.AppStatusHealthy}
	services := []compose.ServiceStatus{{Name: "web", State: "running"}}
	domains := []app.Domain{{ID: "dom_1", AppID: "app_1", ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true}}
	releases := []app.Release{{ID: "rel_1", AppID: "app_1", CommitSHA: "abc123", ComposePath: "compose.yml", Status: app.ReleaseStatusSucceeded, CreatedAt: now}}
	deployments := []app.Deployment{{ID: "dep_1", AppID: "app_1", ReleaseID: "rel_1", Status: app.DeploymentStatusSucceeded, StartedAt: now, FinishedAt: now}}
	events := []app.Event{{ID: "evt_1", AppID: "app_1", Type: "deploy.succeeded", Message: "Deploy succeeded", CreatedAt: now}}

	view := NewAppDetailView(model, services, domains, releases, deployments, events)

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
	if view.Health.RouteStatus != "routed" || view.Health.LatestDeploymentStatus != "succeeded" || view.Health.ServiceStatus != "1 running" {
		t.Fatalf("health = %#v", view.Health)
	}
	if len(view.Actions) == 0 {
		t.Fatal("expected basic action labels")
	}
}

func TestNewAppDetailViewSummarizesLastFailure(t *testing.T) {
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	model := app.App{ID: "app_1", Name: "my-app", NodeID: "local", Status: app.AppStatusFailed}
	deployments := []app.Deployment{{
		ID:           "dep_failed",
		AppID:        "app_1",
		ReleaseID:    "rel_1",
		Status:       app.DeploymentStatusFailed,
		StartedAt:    now,
		FinishedAt:   now,
		ErrorMessage: "stage=start; detail=container exited",
	}}

	view := NewAppDetailView(model, nil, nil, nil, deployments, nil)

	if view.Health.RouteStatus != "unrouted" || view.Health.LatestDeploymentStatus != "failed" {
		t.Fatalf("health = %#v", view.Health)
	}
	if view.Health.LastFailure != "stage=start; detail=container exited" {
		t.Fatalf("last failure = %q", view.Health.LastFailure)
	}
}

func TestNewAppDetailViewUsesNewestAttemptAndFailure(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	deployments := []app.Deployment{
		{ID: "dep_old_failure", Status: app.DeploymentStatusFailed, StartedAt: now, ErrorMessage: "old failure"},
		{ID: "dep_new_failure", Status: app.DeploymentStatusFailed, StartedAt: now.Add(time.Minute), ErrorMessage: "new failure"},
		{ID: "dep_latest_success", Status: app.DeploymentStatusSucceeded, StartedAt: now.Add(2 * time.Minute)},
	}

	view := NewAppDetailView(app.App{ID: "app_1"}, nil, nil, nil, deployments, nil)

	if view.Health.LatestDeploymentStatus != "succeeded" {
		t.Fatalf("latest deployment status = %q", view.Health.LatestDeploymentStatus)
	}
	if view.Health.LastFailure != "new failure" {
		t.Fatalf("last failure = %q", view.Health.LastFailure)
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
	view := NewAppDetailView(app.App{ID: "app_1"}, nil, nil, nil, []app.Deployment{deployment}, nil)

	// Then
	if len(view.Deployments) != 1 {
		t.Fatalf("deployments = %#v", view.Deployments)
	}
	got := view.Deployments[0]
	if got.CommitSHA != deployment.CommitSHA || got.Trigger != string(deployment.Trigger) || got.FailureStage != deployment.FailureStage || got.FailureDetail != deployment.FailureDetail || got.RetryGuidance != deployment.RetryGuidance {
		t.Fatalf("deployment view = %#v", got)
	}
	if !slices.Contains(view.Actions, "redeploy current main") {
		t.Fatalf("actions = %#v, want current-main redeploy wording", view.Actions)
	}
}

func TestNewLogsView(t *testing.T) {
	view := NewLogsView("app_1", "web", "first\nsecond\n")

	if view.AppID != "app_1" || view.ServiceName != "web" {
		t.Fatalf("logs view = %#v", view)
	}
	if strings.Join(view.Lines, ",") != "first,second" {
		t.Fatalf("Lines = %#v", view.Lines)
	}
}

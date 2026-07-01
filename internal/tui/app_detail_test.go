package tui

import "testing"

func TestAppDetailScreenSections(t *testing.T) {
	view := AppDetailView{
		App: AppSummary{ID: "app_1", Name: "my-app", NodeID: "local", Status: "healthy"},
		Services: []ServiceView{
			{Name: "web", State: "running"},
		},
		Domains: []DomainView{
			{DomainName: "example.com", ServiceName: "web", Target: "web:3000", HTTPS: true},
		},
		Releases: []ReleaseView{
			{ID: "rel_1", CommitSHA: "abc123", Status: "succeeded"},
		},
		Deployments: []DeploymentView{
			{ID: "dep_1", ReleaseID: "rel_1", Status: "succeeded"},
		},
		Actions: []string{"restart app", "rollback release"},
	}
	screen := NewAppDetailScreen(view)

	if screen.Metadata().Name != "my-app" || screen.Metadata().Status != "healthy" || screen.Metadata().NodeID != "local" {
		t.Fatalf("metadata = %#v", screen.Metadata())
	}
	if len(screen.Services()) != 1 || screen.Services()[0].Name != "web" {
		t.Fatalf("services = %#v", screen.Services())
	}
	if len(screen.Domains()) != 1 || screen.Domains()[0].Target != "web:3000" {
		t.Fatalf("domains = %#v", screen.Domains())
	}
	if len(screen.Releases()) != 1 || screen.Releases()[0].CommitSHA != "abc123" {
		t.Fatalf("releases = %#v", screen.Releases())
	}
	if len(screen.LatestDeployments(1)) != 1 || screen.LatestDeployments(1)[0].Status != "succeeded" {
		t.Fatalf("deployments = %#v", screen.LatestDeployments(1))
	}
	if len(screen.Actions()) != 2 {
		t.Fatalf("actions = %#v", screen.Actions())
	}
}

func TestAppDetailScreenLimitsLatestDeployments(t *testing.T) {
	screen := NewAppDetailScreen(AppDetailView{
		Deployments: []DeploymentView{
			{ID: "dep_1"},
			{ID: "dep_2"},
			{ID: "dep_3"},
		},
	})

	got := screen.LatestDeployments(2)
	if len(got) != 2 || got[0].ID != "dep_1" || got[1].ID != "dep_2" {
		t.Fatalf("LatestDeployments = %#v", got)
	}
}

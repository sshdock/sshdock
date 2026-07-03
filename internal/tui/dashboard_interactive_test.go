package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInteractiveDashboardModelRendersAndNavigatesApps(t *testing.T) {
	model := NewInteractiveDashboardModel(testDashboardSnapshot(), nil)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = updated.(InteractiveDashboardModel)

	view := model.View()
	for _, want := range []string{
		"Rhumbase",
		"one",
		"web running",
		"first log",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("initial view missing %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "two") || !strings.Contains(view, "worker running") {
		t.Fatalf("down key did not select second app:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "one") || !strings.Contains(view, "web running") {
		t.Fatalf("k key did not select first app:\n%s", view)
	}
}

func TestInteractiveDashboardModelTabsScrollRefreshAndQuit(t *testing.T) {
	refreshCount := 0
	refreshed := testDashboardSnapshot()
	refreshed.AppsByID["one"] = DashboardAppSnapshot{
		Detail: NewAppDetailScreen(AppDetailView{
			App:      AppSummary{ID: "one", Name: "one", NodeID: "local", Status: "healthy"},
			Services: []ServiceView{{Name: "web", State: "restarted"}},
		}),
		Logs: map[string]LogsView{"web": NewLogsView("one", "web", "refreshed log\n")},
	}

	model := NewInteractiveDashboardModel(testDashboardSnapshot(), func(context.Context) (DashboardSnapshot, error) {
		refreshCount++
		return refreshed, nil
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	model = updated.(InteractiveDashboardModel)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "Domains") || !strings.Contains(view, "one.example.com") {
		t.Fatalf("tab did not show domains:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = updated.(InteractiveDashboardModel)
	if model.LogOffset() == 0 {
		t.Fatalf("d key did not scroll logs")
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = updated.(InteractiveDashboardModel)
	if cmd == nil {
		t.Fatal("refresh key returned nil command")
	}
	updated, _ = model.Update(cmd())
	model = updated.(InteractiveDashboardModel)
	if refreshCount != 1 {
		t.Fatalf("refresh count = %d, want 1", refreshCount)
	}
	if view := model.View(); !strings.Contains(view, "web restarted") || !strings.Contains(view, "refreshed log") {
		t.Fatalf("refresh did not replace snapshot:\n%s", view)
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("quit key returned nil command")
	}
}

func testDashboardSnapshot() DashboardSnapshot {
	return DashboardSnapshot{
		Apps: NewAppListScreen(AppListView{Items: []AppListItem{
			{ID: "one", Name: "one", Status: "healthy", NodeID: "local", LatestReleaseStatus: "succeeded", DomainCount: 1},
			{ID: "two", Name: "two", Status: "healthy", NodeID: "local", LatestReleaseStatus: "succeeded", DomainCount: 0},
		}}),
		AppOrder: []string{"one", "two"},
		AppsByID: map[string]DashboardAppSnapshot{
			"one": {
				Detail: NewAppDetailScreen(AppDetailView{
					App:         AppSummary{ID: "one", Name: "one", NodeID: "local", Status: "healthy"},
					Services:    []ServiceView{{Name: "web", State: "running"}},
					Domains:     []DomainView{{DomainName: "one.example.com", Target: "web:3000", HTTPS: true}},
					Releases:    []ReleaseView{{ID: "rel_one", Status: "succeeded", CommitSHA: "abc123"}},
					Deployments: []DeploymentView{{ID: "dep_one", Status: "succeeded", ReleaseID: "rel_one"}},
				}),
				Logs: map[string]LogsView{"web": NewLogsView("one", "web", "first log\nsecond log\nthird log\nfourth log\nfifth log\nsixth log\n")},
			},
			"two": {
				Detail: NewAppDetailScreen(AppDetailView{
					App:      AppSummary{ID: "two", Name: "two", NodeID: "local", Status: "healthy"},
					Services: []ServiceView{{Name: "worker", State: "running"}},
				}),
				Logs: map[string]LogsView{"worker": NewLogsView("two", "worker", "worker log\n")},
			},
		},
	}
}

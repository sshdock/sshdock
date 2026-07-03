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
		"App",
		"State",
		"Release",
		"Doms",
		"one",
		"Summary",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("initial view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "node=local") {
		t.Fatalf("app table should not include node noise:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "two") || !strings.Contains(view, "App two") {
		t.Fatalf("down key did not select second app:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "one") || !strings.Contains(view, "App one") {
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
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 15})
	model = updated.(InteractiveDashboardModel)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "Services") || !strings.Contains(view, "Service") || !strings.Contains(view, "web") {
		t.Fatalf("tab did not show services table:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "Routes") || !strings.Contains(view, "Domain") || !strings.Contains(view, "one.example.com") {
		t.Fatalf("second tab did not show routes table:\n%s", view)
	}

	for i := 0; i < 3; i++ {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
		model = updated.(InteractiveDashboardModel)
	}
	if view := model.View(); !strings.Contains(view, "Logs") || !strings.Contains(view, "first log") {
		t.Fatalf("logs tab did not show logs table:\n%s", view)
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
	if view := model.View(); !strings.Contains(view, "refreshed log") {
		t.Fatalf("refresh did not replace snapshot:\n%s", view)
	}

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("quit key returned nil command")
	}
}

func TestInteractiveDashboardModelFiltersApps(t *testing.T) {
	model := NewInteractiveDashboardModel(testDashboardSnapshot(), nil)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = updated.(InteractiveDashboardModel)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(InteractiveDashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t', 'w', 'o'}})
	model = updated.(InteractiveDashboardModel)

	view := model.View()
	if !strings.Contains(view, "filter: two") {
		t.Fatalf("filter input is not visible:\n%s", view)
	}
	if !strings.Contains(view, "two") || strings.Contains(view, " one ") {
		t.Fatalf("filter did not narrow app list to two:\n%s", view)
	}
	if !strings.Contains(view, "App two") {
		t.Fatalf("filter did not select matching app detail:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(InteractiveDashboardModel)
	view = model.View()
	if !strings.Contains(view, "one") || !strings.Contains(view, "two") {
		t.Fatalf("escape did not clear filter:\n%s", view)
	}
}

func TestInteractiveDashboardModelResponsiveTablesAndTips(t *testing.T) {
	model := NewInteractiveDashboardModel(testDashboardSnapshot(), nil)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	model = updated.(InteractiveDashboardModel)

	view := model.View()
	for _, want := range []string{
		"App",
		"State",
		"Release",
		"Doms",
		"[?] help",
		"[/] filter",
		"[j/k] select",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("wide app table or tips missing %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 22})
	model = updated.(InteractiveDashboardModel)
	view = model.View()
	if strings.Contains(view, "succeeded") || strings.Contains(view, "Doms") {
		t.Fatalf("narrow app table should hide lower-priority columns:\n%s", view)
	}
	if !strings.Contains(view, "App") || !strings.Contains(view, "State") {
		t.Fatalf("narrow app table should keep app and state:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "[type] filter") || !strings.Contains(view, "[esc] clear") {
		t.Fatalf("filter tips missing:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(InteractiveDashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(InteractiveDashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "[esc] apps") {
		t.Fatalf("detail tips missing:\n%s", view)
	}
}

func TestInteractiveDashboardModelDetailTabsRenderTables(t *testing.T) {
	model := NewInteractiveDashboardModel(testDashboardSnapshot(), nil)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	model = updated.(InteractiveDashboardModel)

	wantByTab := map[string][]string{
		"Summary":  {"Field", "Value", "Node", "Services"},
		"Services": {"Service", "State", "web", "running"},
		"Routes":   {"Domain", "Service", "Target", "HTTPS", "one.example.com"},
		"Releases": {"Release", "Status", "Commit", "Created", "rel_one"},
		"Deploys":  {"Deploy", "Status", "Release", "Started", "dep_one"},
		"Logs":     {"Service", "Line", "web", "first log"},
	}

	for _, tab := range dashboardTabs {
		view := model.View()
		for _, want := range wantByTab[tab] {
			if !strings.Contains(view, want) {
				t.Fatalf("%s tab missing %q:\n%s", tab, want, view)
			}
		}
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
		model = updated.(InteractiveDashboardModel)
	}
}

func TestInteractiveDashboardModelJumpKeysAndStatus(t *testing.T) {
	model := NewInteractiveDashboardModel(testDashboardSnapshot(), nil)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 110, Height: 28})
	model = updated.(InteractiveDashboardModel)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "App two") || !strings.Contains(view, "apps 2") {
		t.Fatalf("G did not jump to last app or status is missing:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	model = updated.(InteractiveDashboardModel)
	if view := model.View(); !strings.Contains(view, "App one") || !strings.Contains(view, "focus apps") {
		t.Fatalf("g did not jump to first app or focus status is missing:\n%s", view)
	}
}

func TestInteractiveDashboardModelCompactLayoutIncludesChrome(t *testing.T) {
	model := NewInteractiveDashboardModel(testDashboardSnapshot(), nil)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 22})
	model = updated.(InteractiveDashboardModel)

	view := model.View()
	for _, want := range []string{
		"Rhumbase",
		"Apps",
		"App one",
		"apps 2",
		"[/] filter",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("compact view missing %q:\n%s", want, view)
		}
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
					Domains:     []DomainView{{DomainName: "one.example.com", ServiceName: "web", Target: "web:3000", HTTPS: true}},
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

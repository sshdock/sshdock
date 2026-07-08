package tui

import (
	"context"
	"errors"
	"fmt"
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
		"SSHDock",
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

	for i := 0; i < 4; i++ {
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
	if strings.Contains(view, "Doms") {
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
		"Summary":  {"Field", "Value", "Node", "Route", "Latest deploy", "Services"},
		"Services": {"Service", "State", "web", "running"},
		"Routes":   {"Domain", "Service", "Target", "HTTPS", "one.example.com"},
		"Releases": {"Release", "Status", "Commit", "Created", "rel_one"},
		"Deploys":  {"Deploy", "Status", "Release", "Started", "dep_one"},
		"Events":   {"Type", "Message", "deploy.succeeded", "Deploy succeeded"},
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
		"SSHDock",
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

func TestInteractiveDashboardActionMenuListsAndCloses(t *testing.T) {
	model := NewInteractiveDashboardModelWithActions(testDashboardSnapshot(), nil, &fakeDashboardActions{})
	model = updateDashboardModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})

	model = pressDashboardKey(t, model, "a")
	view := model.View()
	for _, want := range []string{
		"Actions",
		"restart app",
		"restart service",
		"redeploy latest",
		"rollback release",
		"attach domain",
		"detach domain",
		"remove app",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("action menu missing %q:\n%s", want, view)
		}
	}

	model = pressDashboardKey(t, model, "esc")
	if view := model.View(); strings.Contains(view, "restart service") {
		t.Fatalf("escape did not close action menu:\n%s", view)
	}
}

func TestInteractiveDashboardActionsCallBackendWithSelectedArguments(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want string
	}{
		{
			name: "restart app",
			keys: []string{"a", "enter"},
			want: "restart-app one",
		},
		{
			name: "restart service",
			keys: []string{"a", "down", "enter", "enter"},
			want: "restart-service one web",
		},
		{
			name: "redeploy latest",
			keys: []string{"a", "down", "down", "enter"},
			want: "redeploy one",
		},
		{
			name: "rollback release",
			keys: []string{"a", "down", "down", "down", "enter", "enter"},
			want: "rollback one rel_one",
		},
		{
			name: "attach domain",
			keys: append([]string{"a", "down", "down", "down", "down", "enter"}, append(dashboardRuneKeys("web two.example.com 8080"), "enter")...),
			want: "attach one web two.example.com 8080",
		},
		{
			name: "detach domain",
			keys: []string{"a", "down", "down", "down", "down", "down", "enter", "enter"},
			want: "detach one one.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions := &fakeDashboardActions{}
			refreshCount := 0
			model := NewInteractiveDashboardModelWithActions(testDashboardSnapshot(), func(context.Context) (DashboardSnapshot, error) {
				refreshCount++
				return actionRefreshedSnapshot(), nil
			}, actions)
			model = updateDashboardModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})

			var cmd tea.Cmd
			for _, key := range tt.keys {
				model, cmd = pressDashboardKeyWithCmd(t, model, key)
			}
			if cmd == nil {
				t.Fatalf("%s did not return action command", tt.name)
			}
			model = updateDashboardModel(t, model, cmd())

			if got := strings.Join(actions.calls, "\n"); got != tt.want {
				t.Fatalf("calls = %q, want %q", got, tt.want)
			}
			if refreshCount != 1 {
				t.Fatalf("refresh count = %d, want 1", refreshCount)
			}
			view := model.View()
			if !strings.Contains(view, "healthy after action") || !strings.Contains(view, "complete") {
				t.Fatalf("successful action did not refresh snapshot or show status:\n%s", view)
			}
		})
	}
}

func TestInteractiveDashboardRemoveRequiresExactAppName(t *testing.T) {
	actions := &fakeDashboardActions{}
	model := NewInteractiveDashboardModelWithActions(testDashboardSnapshot(), func(context.Context) (DashboardSnapshot, error) {
		return actionRefreshedSnapshot(), nil
	}, actions)
	model = updateDashboardModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})

	for _, key := range []string{"a", "down", "down", "down", "down", "down", "down", "enter"} {
		model = pressDashboardKey(t, model, key)
	}
	for _, key := range dashboardRuneKeys("wrong") {
		model = pressDashboardKey(t, model, key)
	}
	model, cmd := pressDashboardKeyWithCmd(t, model, "enter")
	if cmd != nil {
		t.Fatal("wrong app-name confirmation returned command")
	}
	if len(actions.calls) != 0 {
		t.Fatalf("wrong confirmation called backend: %#v", actions.calls)
	}
	if view := model.View(); !strings.Contains(view, "type app name exactly") || !strings.Contains(view, "Docker volumes stay") || !strings.Contains(view, "confirmation did not match") {
		t.Fatalf("wrong confirmation did not keep prompt and show error:\n%s", view)
	}

	for _, key := range dashboardRuneKeys("one") {
		model = pressDashboardKey(t, model, key)
	}
	model, cmd = pressDashboardKeyWithCmd(t, model, "enter")
	if cmd == nil {
		t.Fatal("correct app-name confirmation returned nil command")
	}
	model = updateDashboardModel(t, model, cmd())
	if got := strings.Join(actions.calls, "\n"); got != "remove one" {
		t.Fatalf("calls = %q, want remove one", got)
	}
}

func TestInteractiveDashboardFailedActionPreservesSnapshotAndShowsError(t *testing.T) {
	actions := &fakeDashboardActions{errByCall: map[string]error{"restart-app one": errors.New("compose restart failed")}}
	refreshCount := 0
	model := NewInteractiveDashboardModelWithActions(testDashboardSnapshot(), func(context.Context) (DashboardSnapshot, error) {
		refreshCount++
		return actionRefreshedSnapshot(), nil
	}, actions)
	model = updateDashboardModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})

	model, cmd := pressDashboardKeysWithFinalCmd(t, model, []string{"a", "enter"})
	if cmd == nil {
		t.Fatal("restart app returned nil command")
	}
	model = updateDashboardModel(t, model, cmd())

	if refreshCount != 0 {
		t.Fatalf("refresh count = %d, want 0 after failed action", refreshCount)
	}
	view := model.View()
	if !strings.Contains(view, "one healthy on local") || strings.Contains(view, "healthy after action") {
		t.Fatalf("failed action did not preserve current snapshot:\n%s", view)
	}
	if !strings.Contains(view, "compose restart failed") {
		t.Fatalf("failed action did not show error:\n%s", view)
	}
}

func TestInteractiveDashboardLogsFollowRefreshesOnlyWhileEnabled(t *testing.T) {
	refreshCount := 0
	model := NewInteractiveDashboardModelWithActions(testDashboardSnapshot(), func(context.Context) (DashboardSnapshot, error) {
		refreshCount++
		return actionRefreshedSnapshot(), nil
	}, &fakeDashboardActions{})
	model = updateDashboardModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})
	for i := 0; i < len(dashboardTabs)-1; i++ {
		model = pressDashboardKey(t, model, "tab")
	}

	model, cmd := pressDashboardKeyWithCmd(t, model, "f")
	if cmd == nil {
		t.Fatal("follow toggle on returned nil tick command")
	}
	if !strings.Contains(model.View(), "follow on") {
		t.Fatalf("follow status missing:\n%s", model.View())
	}

	model, cmd = updateDashboardModelWithCmd(t, model, dashboardFollowTickMsg{})
	if cmd == nil {
		t.Fatal("follow tick while enabled returned nil refresh command")
	}
	model, cmd = updateDashboardModelWithCmd(t, model, cmd())
	if refreshCount != 1 {
		t.Fatalf("refresh count = %d, want 1", refreshCount)
	}
	if cmd == nil {
		t.Fatal("successful follow refresh did not schedule next tick")
	}

	model = pressDashboardKey(t, model, "f")
	model, cmd = updateDashboardModelWithCmd(t, model, dashboardFollowTickMsg{})
	if cmd != nil {
		t.Fatal("follow tick while disabled returned command")
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
					Events:      []EventView{{Type: "deploy.succeeded", Message: "Deploy succeeded"}},
					Health:      HealthSummary{RouteStatus: "routed", LatestDeploymentStatus: "succeeded", ServiceStatus: "1 running"},
				}),
				Logs: map[string]LogsView{"web": NewLogsView("one", "web", "first log\nsecond log\nthird log\nfourth log\nfifth log\nsixth log\n")},
			},
			"two": {
				Detail: NewAppDetailScreen(AppDetailView{
					App:      AppSummary{ID: "two", Name: "two", NodeID: "local", Status: "healthy"},
					Services: []ServiceView{{Name: "worker", State: "running"}},
					Health:   HealthSummary{RouteStatus: "unrouted", LatestDeploymentStatus: "-", ServiceStatus: "1 running"},
				}),
				Logs: map[string]LogsView{"worker": NewLogsView("two", "worker", "worker log\n")},
			},
		},
	}
}

func actionRefreshedSnapshot() DashboardSnapshot {
	snapshot := testDashboardSnapshot()
	item := snapshot.Apps.view.Items[0]
	item.Status = "healthy after action"
	snapshot.Apps.view.Items[0] = item
	appSnapshot := snapshot.AppsByID["one"]
	appSnapshot.Detail = NewAppDetailScreen(AppDetailView{
		App:      AppSummary{ID: "one", Name: "one", NodeID: "local", Status: "healthy after action"},
		Services: []ServiceView{{Name: "web", State: "running"}},
	})
	snapshot.AppsByID["one"] = appSnapshot
	return snapshot
}

func dashboardRuneKeys(value string) []string {
	keys := make([]string, 0, len(value))
	for _, r := range value {
		keys = append(keys, string(r))
	}
	return keys
}

func pressDashboardKeysWithFinalCmd(t *testing.T, model InteractiveDashboardModel, keys []string) (InteractiveDashboardModel, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, key := range keys {
		model, cmd = pressDashboardKeyWithCmd(t, model, key)
	}
	return model, cmd
}

func pressDashboardKey(t *testing.T, model InteractiveDashboardModel, key string) InteractiveDashboardModel {
	t.Helper()
	updated, _ := pressDashboardKeyWithCmd(t, model, key)
	return updated
}

func pressDashboardKeyWithCmd(t *testing.T, model InteractiveDashboardModel, key string) (InteractiveDashboardModel, tea.Cmd) {
	t.Helper()
	return updateDashboardModelWithCmd(t, model, dashboardKeyMsg(key))
}

func updateDashboardModel(t *testing.T, model InteractiveDashboardModel, msg tea.Msg) InteractiveDashboardModel {
	t.Helper()
	updated, _ := updateDashboardModelWithCmd(t, model, msg)
	return updated
}

func updateDashboardModelWithCmd(t *testing.T, model InteractiveDashboardModel, msg tea.Msg) (InteractiveDashboardModel, tea.Cmd) {
	t.Helper()
	updated, cmd := model.Update(msg)
	typed, ok := updated.(InteractiveDashboardModel)
	if !ok {
		t.Fatalf("updated model = %T, want InteractiveDashboardModel", updated)
	}
	return typed, cmd
}

func dashboardKeyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

type fakeDashboardActions struct {
	calls     []string
	errByCall map[string]error
}

func (f *fakeDashboardActions) record(call string) error {
	f.calls = append(f.calls, call)
	if f.errByCall == nil {
		return nil
	}
	return f.errByCall[call]
}

func (f *fakeDashboardActions) RestartApp(appName string) error {
	return f.record(fmt.Sprintf("restart-app %s", appName))
}

func (f *fakeDashboardActions) RestartService(appName string, serviceName string) error {
	return f.record(fmt.Sprintf("restart-service %s %s", appName, serviceName))
}

func (f *fakeDashboardActions) RedeployApp(appName string) error {
	return f.record(fmt.Sprintf("redeploy %s", appName))
}

func (f *fakeDashboardActions) RollbackApp(appName string, releaseID string) error {
	return f.record(fmt.Sprintf("rollback %s %s", appName, releaseID))
}

func (f *fakeDashboardActions) AttachDomain(appName string, serviceName string, domainName string, port int) error {
	return f.record(fmt.Sprintf("attach %s %s %s %d", appName, serviceName, domainName, port))
}

func (f *fakeDashboardActions) DetachDomain(appName string, domainName string) error {
	return f.record(fmt.Sprintf("detach %s %s", appName, domainName))
}

func (f *fakeDashboardActions) RemoveApp(appName string) error {
	return f.record(fmt.Sprintf("remove %s", appName))
}

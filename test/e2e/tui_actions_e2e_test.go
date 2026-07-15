//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/cli"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
	"github.com/sshdock/sshdock/internal/tui"
)

func TestTUIActionsEndToEnd(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "sshdock.db")
	sqlite, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer sqlite.Close()

	appName := "tui-actions-app"
	appsDir := filepath.Join(tmp, "apps")
	appDir := filepath.Join(appsDir, appName)
	repoPath := filepath.Join(appDir, "repo.git")
	worktreePath := filepath.Join(appDir, "worktree")
	composePath := filepath.Join(worktreePath, "compose.yml")
	for _, path := range []string{repoPath, worktreePath} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", path, err)
		}
	}
	if err := os.WriteFile(composePath, []byte("services:\n  web:\n    image: example/web:latest\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}

	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	seedTUIActionState(t, sqlite, appName, repoPath, worktreePath, composePath, now)

	composeRunner := &compose.FakeRunner{
		Services: []compose.ServiceStatus{
			{Name: "web", State: "running"},
			{Name: "worker", State: "running"},
		},
		LogOutput: "web log\n",
	}
	fakeRouter := router.NewFakeRouter()
	currentTime := now.Add(time.Hour)
	backend := cli.NewStoreBackend(sqlite, cli.StoreBackendConfig{
		NodeID:              "local",
		AppsDir:             appsDir,
		Router:              fakeRouter,
		RecoveryRunner:      composeRunner,
		CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) { return "new", nil }),
		Now: func() time.Time {
			value := currentTime
			currentTime = currentTime.Add(time.Second)
			return value
		},
	})
	actions := tuiCLIActionAdapter{backend: backend}
	handler := tui.NewDashboardHandler(sqlite, composeRunner)
	snapshot, err := handler.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	model := tui.NewInteractiveDashboardModelWithActions(snapshot, handler.Snapshot, actions)
	model = updateTUIActionsModel(t, model, tea.WindowSizeMsg{Width: 132, Height: 34})

	for i := 0; i < len([]string{"Summary", "Services", "Routes", "Releases", "Deploys"}); i++ {
		model = pressTUIActionsKey(t, model, "tab")
	}
	if view := model.View(); !strings.Contains(view, "Events") || !strings.Contains(view, "deploy.succeeded") {
		t.Fatalf("events tab missing persisted event:\n%s", view)
	}

	model = runTUIAction(t, model, []string{"a", "enter"})
	if got := composeRunner.StartRequests[0]; got.AppName != appName || got.ComposePath != composePath {
		t.Fatalf("start app request = %#v", got)
	}

	model = runTUIAction(t, model, []string{"a", "down", "enter"})
	if got := composeRunner.StopRequests[0]; got.AppName != appName || got.ComposePath != composePath {
		t.Fatalf("stop app request = %#v", got)
	}

	model = runTUIAction(t, model, []string{"a", "down", "down", "enter"})
	if got := composeRunner.RestartRequests[0]; got.AppName != appName || got.ServiceName != "" || got.ComposePath != composePath {
		t.Fatalf("restart app request = %#v", got)
	}

	model = runTUIAction(t, model, []string{"a", "down", "down", "down", "enter", "enter"})
	if got := composeRunner.RestartRequests[1]; got.AppName != appName || got.ServiceName != "web" {
		t.Fatalf("restart service request = %#v", got)
	}

	model = runTUIAction(t, model, []string{"a", "down", "down", "down", "down", "enter"})
	if len(composeRunner.DeployRequests) != 1 || composeRunner.DeployRequests[0].ReleaseID != "rel_new" {
		t.Fatalf("redeploy requests = %#v", composeRunner.DeployRequests)
	}

	model = runTUIAction(t, model, []string{"a", "down", "down", "down", "down", "down", "enter", "enter"})
	if len(composeRunner.DeployRequests) != 2 || composeRunner.DeployRequests[1].ReleaseID != "rel_old" {
		t.Fatalf("rollback requests = %#v", composeRunner.DeployRequests)
	}

	model = runTUIAction(t, model, append([]string{"a", "down", "down", "down", "down", "down", "down", "enter"}, append(tuiActionRuneKeys("web new.example.com 8080"), "enter")...))
	domains, err := sqlite.ListDomainsByApp(ctx, appName)
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if !hasDomain(domains, "new.example.com") {
		t.Fatalf("domains after attach = %#v", domains)
	}

	model = runTUIAction(t, model, []string{"a", "down", "down", "down", "down", "down", "down", "down", "enter", "enter"})
	domains, err = sqlite.ListDomainsByApp(ctx, appName)
	if err != nil {
		t.Fatalf("ListDomainsByApp after detach: %v", err)
	}
	if hasDomain(domains, "initial.example.com") {
		t.Fatalf("initial domain still present after detach: %#v", domains)
	}

	model = runTUIAction(t, model, append([]string{"a", "down", "down", "down", "down", "down", "down", "down", "down", "enter"}, append(tuiActionRuneKeys(appName), "enter")...))
	_ = model
	if _, err := sqlite.GetApp(ctx, appName); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetApp after remove error = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(appDir); !os.IsNotExist(err) {
		t.Fatalf("app dir stat after remove = %v, want not exist", err)
	}
	if len(composeRunner.RemoveRequests) != 1 {
		t.Fatalf("remove requests = %#v", composeRunner.RemoveRequests)
	}
	remove := composeRunner.RemoveRequests[0]
	if remove.AppName != appName || remove.ProjectDir != worktreePath || remove.ComposePath != composePath {
		t.Fatalf("remove request = %#v", remove)
	}
	routes, err := fakeRouter.Routes(ctx)
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}
	if len(routes) != 0 {
		t.Fatalf("routes after remove = %#v, want none", routes)
	}
}

func seedTUIActionState(t *testing.T, sqlite *store.SQLiteStore, appName string, repoPath string, worktreePath string, composePath string, now time.Time) {
	t.Helper()
	ctx := context.Background()
	model := app.App{
		ID:           appName,
		Name:         appName,
		NodeID:       "local",
		RepoPath:     repoPath,
		WorktreePath: worktreePath,
		Status:       app.AppStatusHealthy,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := sqlite.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	releases := []app.Release{
		{ID: "rel_old", AppID: appName, CommitSHA: "old", ComposePath: composePath, Status: app.ReleaseStatusSucceeded, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
		{ID: "rel_new", AppID: appName, CommitSHA: "new", ComposePath: composePath, Status: app.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now},
	}
	for _, release := range releases {
		if err := sqlite.CreateRelease(ctx, release); err != nil {
			t.Fatalf("CreateRelease: %v", err)
		}
	}
	if err := sqlite.CreateDeployment(ctx, app.Deployment{ID: "dep_new", AppID: appName, ReleaseID: "rel_new", Status: app.DeploymentStatusSucceeded, StartedAt: now, FinishedAt: now}); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if err := sqlite.AttachDomain(ctx, app.Domain{ID: "dom_initial", AppID: appName, ServiceName: "web", DomainName: "initial.example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	if err := sqlite.CreateEvent(ctx, app.Event{ID: "evt_deploy", AppID: appName, Type: "deploy.succeeded", Message: "Deploy succeeded", CreatedAt: now}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
}

func runTUIAction(t *testing.T, model tui.InteractiveDashboardModel, keys []string) tui.InteractiveDashboardModel {
	t.Helper()
	var cmd tea.Cmd
	for _, key := range keys {
		model, cmd = pressTUIActionsKeyWithCmd(t, model, key)
	}
	if cmd == nil {
		t.Fatalf("keys %v returned nil command", keys)
	}
	return updateTUIActionsModel(t, model, cmd())
}

func pressTUIActionsKey(t *testing.T, model tui.InteractiveDashboardModel, key string) tui.InteractiveDashboardModel {
	t.Helper()
	model, _ = pressTUIActionsKeyWithCmd(t, model, key)
	return model
}

func pressTUIActionsKeyWithCmd(t *testing.T, model tui.InteractiveDashboardModel, key string) (tui.InteractiveDashboardModel, tea.Cmd) {
	t.Helper()
	return updateTUIActionsModelWithCmd(t, model, tuiActionKeyMsg(key))
}

func updateTUIActionsModel(t *testing.T, model tui.InteractiveDashboardModel, msg tea.Msg) tui.InteractiveDashboardModel {
	t.Helper()
	model, _ = updateTUIActionsModelWithCmd(t, model, msg)
	return model
}

func updateTUIActionsModelWithCmd(t *testing.T, model tui.InteractiveDashboardModel, msg tea.Msg) (tui.InteractiveDashboardModel, tea.Cmd) {
	t.Helper()
	updated, cmd := model.Update(msg)
	typed, ok := updated.(tui.InteractiveDashboardModel)
	if !ok {
		t.Fatalf("updated model = %T, want tui.InteractiveDashboardModel", updated)
	}
	return typed, cmd
}

func tuiActionKeyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func tuiActionRuneKeys(value string) []string {
	keys := make([]string, 0, len(value))
	for _, r := range value {
		keys = append(keys, string(r))
	}
	return keys
}

func hasDomain(domains []app.Domain, domainName string) bool {
	for _, domain := range domains {
		if domain.DomainName == domainName {
			return true
		}
	}
	return false
}

type tuiCLIActionAdapter struct {
	backend *cli.StoreBackend
}

func (a tuiCLIActionAdapter) StartApp(appName string) error {
	return a.backend.StartApp(appName)
}

func (a tuiCLIActionAdapter) StopApp(appName string) error {
	return a.backend.StopApp(appName)
}

func (a tuiCLIActionAdapter) RestartApp(appName string) error {
	return a.backend.RestartApp(appName)
}

func (a tuiCLIActionAdapter) RestartService(appName string, serviceName string) error {
	return a.backend.RestartService(appName, serviceName)
}

func (a tuiCLIActionAdapter) RedeployApp(appName string) error {
	return a.backend.RedeployApp(appName)
}

func (a tuiCLIActionAdapter) RollbackApp(appName string, releaseID string) error {
	return a.backend.RollbackApp(appName, releaseID)
}

func (a tuiCLIActionAdapter) AttachDomain(appName string, serviceName string, domainName string, port int) error {
	return a.backend.AttachDomain(cli.Domain{
		AppName:     appName,
		ServiceName: serviceName,
		DomainName:  domainName,
		Port:        port,
	})
}

func (a tuiCLIActionAdapter) DetachDomain(appName string, domainName string) error {
	return a.backend.DetachDomain(appName, domainName)
}

func (a tuiCLIActionAdapter) RemoveApp(appName string) error {
	return a.backend.RemoveApp(appName)
}

func (a tuiCLIActionAdapter) String() string {
	return fmt.Sprintf("%T", a.backend)
}

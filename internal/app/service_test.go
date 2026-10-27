package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestServiceRestartAppAndServiceUseCurrentWorktreeComposeEntry(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	worktreePath := t.TempDir()
	composePath := filepath.Join(worktreePath, "compose.yml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write current Compose file: %v", err)
	}
	runner := &fakeRecoveryRunner{}
	service := NewService(store, WithClock(func() time.Time { return now }), WithRecoveryRunner(runner))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: worktreePath, Status: AppStatusHealthy}
	store.releases["rel_bad"] = Release{ID: "rel_bad", AppID: "app_1", CommitSHA: "bad", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusFailed, CreatedAt: now.Add(-time.Hour)}
	store.releases["rel_good"] = Release{ID: "rel_good", AppID: "app_1", CommitSHA: "good", ComposePath: "/historical/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	// When
	if err := service.RestartApp(ctx, "app_1"); err != nil {
		t.Fatalf("RestartApp: %v", err)
	}
	if err := service.RestartService(ctx, "app_1", "web"); err != nil {
		t.Fatalf("RestartService: %v", err)
	}

	if len(runner.restarts) != 2 {
		t.Fatalf("restart requests = %#v", runner.restarts)
	}
	appRestart := runner.restarts[0]
	if appRestart.AppName != "app_1" || appRestart.ProjectDir != worktreePath || appRestart.ComposePath != composePath || appRestart.ServiceName != "" {
		t.Fatalf("app restart request = %#v", appRestart)
	}
	serviceRestart := runner.restarts[1]
	if serviceRestart.ServiceName != "web" {
		t.Fatalf("service restart request = %#v", serviceRestart)
	}
	assertEventTypes(t, store.events["app_1"], []string{"restart.started", "restart.succeeded", "service.restart.started", "service.restart.succeeded"})
}

type fakeServiceStore struct {
	apps     map[string]App
	releases map[string]Release
	events   map[string][]Event
}

func testComposeWorktree(t *testing.T) string {
	t.Helper()
	worktreePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile Compose: %v", err)
	}
	return worktreePath
}

type fakeRecoveryRunner struct {
	starts   []compose.LifecycleRequest
	stops    []compose.LifecycleRequest
	restarts []compose.RestartRequest

	startErr   error
	stopErr    error
	restartErr error
}

type configResolveRequest struct {
	appID string
}

type fakeConfigResolver struct {
	env      map[string]string
	err      error
	requests []configResolveRequest
}

func (f *fakeConfigResolver) ResolveEnv(ctx context.Context, appID string) (map[string]string, error) {
	f.requests = append(f.requests, configResolveRequest{appID: appID})
	return f.env, f.err
}

func (f *fakeRecoveryRunner) Start(_ context.Context, request compose.LifecycleRequest) error {
	f.starts = append(f.starts, request)
	return f.startErr
}

func (f *fakeRecoveryRunner) Stop(_ context.Context, request compose.LifecycleRequest) error {
	f.stops = append(f.stops, request)
	return f.stopErr
}

func (f *fakeRecoveryRunner) Restart(_ context.Context, request compose.RestartRequest) error {
	f.restarts = append(f.restarts, request)
	return f.restartErr
}

func assertEventTypes(t *testing.T, events []Event, want []string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("events = %#v, want types %#v", events, want)
	}
	for i := range want {
		if events[i].Type != want[i] {
			t.Fatalf("event[%d] type = %q, want %q; events = %#v", i, events[i].Type, want[i], events)
		}
	}
}

func newFakeServiceStore() *fakeServiceStore {
	return &fakeServiceStore{
		apps:     map[string]App{},
		releases: map[string]Release{},
		events:   map[string][]Event{},
	}
}

func writeCurrentCompose(t *testing.T) (string, string) {
	t.Helper()
	worktreePath := t.TempDir()
	composePath := filepath.Join(worktreePath, "compose.yml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write current Compose file: %v", err)
	}
	return worktreePath, composePath
}

func (f *fakeServiceStore) GetApp(_ context.Context, id string) (App, error) {
	model, ok := f.apps[id]
	if !ok {
		return App{}, errors.New("not found")
	}
	return model, nil
}

func (f *fakeServiceStore) CreateEvent(_ context.Context, model Event) error {
	f.events[model.AppID] = append(f.events[model.AppID], model)
	return nil
}

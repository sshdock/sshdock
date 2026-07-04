package gitrecv

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
	"github.com/iketiunn/rumbase/internal/compose"
	"github.com/iketiunn/rumbase/internal/router"
	"github.com/iketiunn/rumbase/internal/store"
)

func TestPostReceiveHandlerCreatesReleaseAndSucceededDeployment(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rhumbase.db")
	sqlite := newHookTestStore(t, ctx, dbPath)
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	runner := &compose.FakeRunner{}
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: runner,
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, repoPath string, gotWorktreePath string, commitSHA string) error {
			if repoPath != "/apps/my-app/repo.git" {
				t.Fatalf("repoPath = %q", repoPath)
			}
			if gotWorktreePath != worktreePath {
				t.Fatalf("worktreePath = %q", gotWorktreePath)
			}
			if commitSHA != "abc123" {
				t.Fatalf("commitSHA = %q", commitSHA)
			}
			writeHookComposeFixture(t, gotWorktreePath)
			return nil
		}),
		Now: func() time.Time { return now },
	})

	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", worktreePath, strings.NewReader("oldsha abc123 refs/heads/main\n"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	releases, err := sqlite.ListReleasesByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListReleasesByApp: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %#v", releases)
	}
	if releases[0].ID != "rel_abc123" || releases[0].CommitSHA != "abc123" || releases[0].ComposePath != filepath.Join(worktreePath, "compose.yml") {
		t.Fatalf("release = %#v", releases[0])
	}
	if releases[0].Status != app.ReleaseStatusSucceeded {
		t.Fatalf("release status = %q, want %q", releases[0].Status, app.ReleaseStatusSucceeded)
	}

	if len(runner.DeployRequests) != 1 {
		t.Fatalf("DeployRequests = %#v", runner.DeployRequests)
	}
	request := runner.DeployRequests[0]
	if request.AppName != "my-app" || request.ProjectDir != worktreePath || request.ReleaseID != "rel_abc123" || request.CommitSHA != "abc123" {
		t.Fatalf("DeployRequest = %#v", request)
	}

	status := queryDeploymentStatus(t, dbPath, "dep_abc123")
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
	errorMessage := queryDeploymentError(t, dbPath, "dep_abc123")
	if errorMessage != "" {
		t.Fatalf("deployment error = %q, want empty", errorMessage)
	}

	model, err := sqlite.GetApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if model.Status != app.AppStatusHealthy {
		t.Fatalf("app status = %q, want %q", model.Status, app.AppStatusHealthy)
	}

	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.started", "deploy.succeeded"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
}

func TestPostReceiveHandlerAutoRoutesAfterSuccessfulDeploy(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rhumbase.db")
	sqlite := newHookTestStore(t, ctx, dbPath)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "rhumbase.example.com",
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	routeSyncer := &fakeHookRouteSyncer{}
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: &compose.FakeRunner{},
		Router: routeSyncer,
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeHookCompose(t, gotWorktreePath, `
services:
  web:
    image: example/web:latest
    ports:
      - "127.0.0.1:3100:80"
`)
			return nil
		}),
		Now: func() time.Time { return now },
	})

	if err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", worktreePath, strings.NewReader("oldsha abc123 refs/heads/main\n")); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	domains, err := sqlite.ListDomainsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	wantDomain := app.Domain{
		ID:          "dom_my_app_my_app_example_com",
		AppID:       "my-app",
		ServiceName: "web",
		DomainName:  "my-app.example.com",
		Port:        3100,
		HTTPS:       true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if len(domains) != 1 || domains[0] != wantDomain {
		t.Fatalf("domains = %#v, want [%#v]", domains, wantDomain)
	}
	if len(routeSyncer.Syncs) != 1 {
		t.Fatalf("router syncs = %#v, want one sync", routeSyncer.Syncs)
	}
	wantRoutes := []router.Route{{AppID: "my-app", ServiceName: "web", DomainName: "my-app.example.com", Port: 3100, HTTPS: true}}
	if !reflect.DeepEqual(routeSyncer.Syncs[0], wantRoutes) {
		t.Fatalf("router sync = %#v, want %#v", routeSyncer.Syncs[0], wantRoutes)
	}

	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.started", "deploy.succeeded", "route.auto_attached", "router.reloaded"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
}

func TestPostReceiveHandlerDoesNotAutoRouteFailedDeploy(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rhumbase.db")
	sqlite := newHookTestStore(t, ctx, dbPath)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "rhumbase.example.com",
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	routeSyncer := &fakeHookRouteSyncer{}
	clock := newHookClock(now)
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: &compose.FakeRunner{DeployErr: errors.New("compose failed")},
		Router: routeSyncer,
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeHookCompose(t, gotWorktreePath, `
services:
  web:
    image: example/web:latest
    ports:
      - "3100:80"
`)
			return nil
		}),
		Now: clock.Now,
	})

	if err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", worktreePath, strings.NewReader("oldsha abc123 refs/heads/main\n")); err == nil {
		t.Fatal("Handle error = nil, want deploy failure")
	}
	domains, err := sqlite.ListDomainsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("domains = %#v, want none", domains)
	}
	if len(routeSyncer.Syncs) != 0 {
		t.Fatalf("router syncs = %#v, want none", routeSyncer.Syncs)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.started", "deploy.failed"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
}

func TestPostReceiveHandlerRecordsAutoRouteSkippedForUnsafeInference(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rhumbase.db")
	sqlite := newHookTestStore(t, ctx, dbPath)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "rhumbase.example.com",
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	routeSyncer := &fakeHookRouteSyncer{}
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: &compose.FakeRunner{},
		Router: routeSyncer,
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeHookCompose(t, gotWorktreePath, `
services:
  api:
    image: example/api:latest
    ports:
      - "4100:80"
  admin:
    image: example/admin:latest
    ports:
      - "4200:80"
`)
			return nil
		}),
		Now: func() time.Time { return now },
	})

	if err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", worktreePath, strings.NewReader("oldsha abc123 refs/heads/main\n")); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	domains, err := sqlite.ListDomainsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("domains = %#v, want none", domains)
	}
	if len(routeSyncer.Syncs) != 0 {
		t.Fatalf("router syncs = %#v, want none", routeSyncer.Syncs)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.started", "deploy.succeeded", "route.auto_skipped"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
	if !strings.Contains(events[2].Message, "ambiguous") || !strings.Contains(events[2].Message, "domains attach") {
		t.Fatalf("skip event = %#v, want actionable ambiguous-route message", events[2])
	}
}

func TestPostReceiveHandlerRecordsAutoRouteSkippedForDNSUnsafeAppName(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rhumbase.db")
	sqlite := newHookTestStoreForApp(t, ctx, dbPath, "bad_app")
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "rhumbase.example.com",
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: &compose.FakeRunner{},
		Router: &fakeHookRouteSyncer{},
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeHookCompose(t, gotWorktreePath, `
services:
  web:
    image: example/web:latest
    ports:
      - "3100:80"
`)
			return nil
		}),
		Now: func() time.Time { return now },
	})

	if err := handler.Handle(ctx, "bad_app", "/apps/bad_app/repo.git", worktreePath, strings.NewReader("oldsha abc123 refs/heads/main\n")); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	domains, err := sqlite.ListDomainsByApp(ctx, "bad_app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("domains = %#v, want none", domains)
	}
	events, err := sqlite.ListEventsByApp(ctx, "bad_app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.started", "deploy.succeeded", "route.auto_skipped"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
	if !strings.Contains(events[2].Message, "DNS label") {
		t.Fatalf("skip event = %#v, want DNS label guidance", events[2])
	}
}

func TestPostReceiveHandlerPassesPriorSuccessfulReleaseSHAsForCleanup(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rhumbase.db")
	sqlite := newHookTestStore(t, ctx, dbPath)
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	baseTime := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	for index, release := range []struct {
		sha    string
		status app.ReleaseStatus
	}{
		{sha: "oldest-success", status: app.ReleaseStatusSucceeded},
		{sha: "failed-release", status: app.ReleaseStatusFailed},
		{sha: "middle-success", status: app.ReleaseStatusSucceeded},
		{sha: "newest-success", status: app.ReleaseStatusSucceeded},
	} {
		createdAt := baseTime.Add(time.Duration(index) * time.Minute)
		if err := sqlite.CreateRelease(ctx, app.Release{
			ID:          "rel_" + release.sha,
			AppID:       "my-app",
			CommitSHA:   release.sha,
			ComposePath: filepath.Join(worktreePath, "compose.yml"),
			Status:      release.status,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		}); err != nil {
			t.Fatalf("CreateRelease %s: %v", release.sha, err)
		}
	}

	runner := &compose.FakeRunner{}
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: runner,
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeHookComposeFixture(t, gotWorktreePath)
			return nil
		}),
		Now: func() time.Time { return baseTime.Add(10 * time.Minute) },
	})

	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", worktreePath, strings.NewReader("oldsha abc123 refs/heads/main\n"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(runner.DeployRequests) != 1 {
		t.Fatalf("DeployRequests = %#v", runner.DeployRequests)
	}

	want := []string{"newest-success", "middle-success", "oldest-success"}
	if !reflect.DeepEqual(runner.DeployRequests[0].SuccessfulReleaseSHAs, want) {
		t.Fatalf("SuccessfulReleaseSHAs = %#v, want %#v", runner.DeployRequests[0].SuccessfulReleaseSHAs, want)
	}
}

func TestPostReceiveHandlerMarksDeploymentFailedWhenDeployFails(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rhumbase.db")
	sqlite := newHookTestStore(t, ctx, dbPath)
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	failure := errors.New("fake deploy failed")
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: &compose.FakeRunner{DeployErr: failure},
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeHookComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", worktreePath, strings.NewReader("oldsha abc123 refs/heads/main\n"))
	if !errors.Is(err, failure) {
		t.Fatalf("Handle error = %v, want %v", err, failure)
	}

	status := queryDeploymentStatus(t, dbPath, "dep_abc123")
	if status != string(app.DeploymentStatusFailed) {
		t.Fatalf("deployment status = %q", status)
	}
	errorMessage := queryDeploymentError(t, dbPath, "dep_abc123")
	if !strings.Contains(errorMessage, "fake deploy failed") {
		t.Fatalf("deployment error = %q", errorMessage)
	}

	model, getErr := sqlite.GetApp(ctx, "my-app")
	if getErr != nil {
		t.Fatalf("GetApp: %v", getErr)
	}
	if model.Status != app.AppStatusFailed {
		t.Fatalf("app status = %q, want %q", model.Status, app.AppStatusFailed)
	}

	releases, listErr := sqlite.ListReleasesByApp(ctx, "my-app")
	if listErr != nil {
		t.Fatalf("ListReleasesByApp: %v", listErr)
	}
	if len(releases) != 1 || releases[0].Status != app.ReleaseStatusFailed {
		t.Fatalf("releases = %#v, want one failed release", releases)
	}

	events, listEventsErr := sqlite.ListEventsByApp(ctx, "my-app")
	if listEventsErr != nil {
		t.Fatalf("ListEventsByApp: %v", listEventsErr)
	}
	wantTypes := []string{"deploy.started", "deploy.failed"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
}

func TestPostReceiveHandlerRecordsCleanupFailureEventWithoutFailingDeploy(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rhumbase.db")
	sqlite := newHookTestStore(t, ctx, dbPath)
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: cleanupWarningRunner{},
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeHookComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", worktreePath, strings.NewReader("oldsha abc123 refs/heads/main\n"))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	status := queryDeploymentStatus(t, dbPath, "dep_abc123")
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.started", "cleanup.failed", "deploy.succeeded"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
	if !strings.Contains(events[1].Message, "rhumbase/my-app/web:old-1") || !strings.Contains(events[1].Message, "image is in use") {
		t.Fatalf("cleanup event = %#v", events[1])
	}
}

type cleanupWarningRunner struct{}

func (cleanupWarningRunner) Validate(context.Context, string) (compose.ValidationResult, error) {
	return compose.ValidationResult{}, nil
}

func (cleanupWarningRunner) Deploy(ctx context.Context, request compose.DeployRequest) error {
	return request.CleanupRecorder.RecordCleanupFailure(ctx, compose.CleanupFailure{
		AppName:      request.AppName,
		ServiceName:  "web",
		CommitSHA:    "old-1",
		Image:        "rhumbase/my-app/web:old-1",
		ErrorMessage: "image is in use",
	})
}

func (cleanupWarningRunner) Restart(context.Context, compose.RestartRequest) error {
	return nil
}

func (cleanupWarningRunner) Remove(context.Context, compose.RemoveRequest) error {
	return nil
}

func (cleanupWarningRunner) Status(context.Context, compose.StatusRequest) ([]compose.ServiceStatus, error) {
	return nil, nil
}

func (cleanupWarningRunner) Logs(context.Context, compose.LogsRequest) (string, error) {
	return "", nil
}

func newHookTestStore(t *testing.T, ctx context.Context, dbPath string) *store.SQLiteStore {
	return newHookTestStoreForApp(t, ctx, dbPath, "my-app")
}

func newHookTestStoreForApp(t *testing.T, ctx context.Context, dbPath string, appName string) *store.SQLiteStore {
	t.Helper()

	sqlite, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := sqlite.CreateApp(ctx, app.App{
		ID:           appName,
		Name:         appName,
		NodeID:       "local",
		RepoPath:     "/apps/" + appName + "/repo.git",
		WorktreePath: "/apps/" + appName + "/worktree",
		Status:       app.AppStatusCreated,
		CreatedAt:    time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlite.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	return sqlite
}

func writeHookComposeFixture(t *testing.T, worktreePath string) {
	t.Helper()

	writeHookCompose(t, worktreePath, `
services:
  web:
    image: example/web:latest
`)
}

func writeHookCompose(t *testing.T, worktreePath string, content string) {
	t.Helper()

	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

type fakeHookRouteSyncer struct {
	Syncs [][]router.Route
	Err   error
}

func (f *fakeHookRouteSyncer) SyncRoutes(_ context.Context, routes []router.Route) error {
	copied := append([]router.Route(nil), routes...)
	f.Syncs = append(f.Syncs, copied)
	return f.Err
}

type hookClock struct {
	next time.Time
}

func newHookClock(start time.Time) *hookClock {
	return &hookClock{next: start}
}

func (c *hookClock) Now() time.Time {
	value := c.next
	c.next = c.next.Add(time.Second)
	return value
}

func queryDeploymentStatus(t *testing.T, dbPath string, deploymentID string) string {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	var status string
	if err := db.QueryRow(`select status from deployments where id = ?`, deploymentID).Scan(&status); err != nil {
		t.Fatalf("query deployment status: %v", err)
	}
	return status
}

func queryDeploymentError(t *testing.T, dbPath string, deploymentID string) string {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	var errorMessage string
	if err := db.QueryRow(`select error_message from deployments where id = ?`, deploymentID).Scan(&errorMessage); err != nil {
		t.Fatalf("query deployment error: %v", err)
	}
	return errorMessage
}

func eventTypes(events []app.Event) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

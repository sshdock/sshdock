package gitrecv

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
	"github.com/iketiunn/rumbase/internal/compose"
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

func (cleanupWarningRunner) Status(context.Context, compose.StatusRequest) ([]compose.ServiceStatus, error) {
	return nil, nil
}

func (cleanupWarningRunner) Logs(context.Context, compose.LogsRequest) (string, error) {
	return "", nil
}

func newHookTestStore(t *testing.T, ctx context.Context, dbPath string) *store.SQLiteStore {
	t.Helper()

	sqlite, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := sqlite.CreateApp(ctx, app.App{
		ID:           "my-app",
		Name:         "my-app",
		NodeID:       "local",
		RepoPath:     "/apps/my-app/repo.git",
		WorktreePath: "/apps/my-app/worktree",
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

	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte("services:\n  web:\n    image: example/web:latest\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
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

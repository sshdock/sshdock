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
}

func newHookTestStore(t *testing.T, ctx context.Context, dbPath string) *store.SQLiteStore {
	t.Helper()

	sqlite, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
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

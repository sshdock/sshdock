package gitrecv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

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
		WorktreePath: filepath.Join(filepath.Dir(dbPath), "worktree"),
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

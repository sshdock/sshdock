package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestRunDaemonContextDoesNotDeployAppsOnStartup(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "sshdock.db")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", dbPath)
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	t.Setenv("SSHDOCK_FAKE_COMPOSE_DEPLOY_ERROR", "startup must not deploy")
	now := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	db, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	if err := db.CreateApp(context.Background(), appmodel.App{ID: "app_1", Name: "app_1", Status: appmodel.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := db.CreateRelease(context.Background(), appmodel.Release{ID: "rel_good", AppID: "app_1", CommitSHA: "abc123", ComposePath: "/apps/app_1/worktree/compose.yml", Status: appmodel.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create release: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close SQLite: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer

	code := runDaemonContext(ctx, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	db, err = store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen SQLite: %v", err)
	}
	defer db.Close()
	deployments, err := db.ListDeploymentsByApp(context.Background(), "app_1")
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(deployments) != 0 {
		t.Fatalf("startup deployments = %#v, want none", deployments)
	}
}

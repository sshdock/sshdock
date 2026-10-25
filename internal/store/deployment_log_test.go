package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestOpenSQLiteCurrentSchemaDoesNotNeedWriteLock(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sshdock.db")
	db, err := OpenSQLite(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader, err := db.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	var count int
	if err := reader.QueryRowContext(ctx, "select count(*) from deployments").Scan(&count); err != nil {
		t.Fatal(err)
	}
	// A current-schema open must migrate without requiring this reader to leave.
	query, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatal(err)
	}
	defer query.Close()
	if err := Migrate(ctx, query); err != nil {
		t.Fatalf("current-schema migration attempted to write: %v", err)
	}
}

func TestAppendDeploymentLogWaitsForConcurrentWriter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db := newTestStore(t, ctx)
	now := time.Now()
	if err := createDeploymentLog(ctx, db.db, app.Deployment{ID: "dep_log", AppID: "my-app", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	if _, err := blocker.ExecContext(ctx, "begin immediate"); err != nil {
		t.Fatal(err)
	}
	defer blocker.ExecContext(context.Background(), "rollback")

	done := make(chan error, 1)
	go func() {
		done <- db.AppendDeploymentLog(ctx, "my-app", "dep_log", "after contention\n", now)
	}()
	select {
	case err := <-done:
		t.Fatalf("append returned before the concurrent writer released its lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := blocker.ExecContext(ctx, "rollback"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("append after lock release: %v", err)
	}
	log, err := db.DeploymentLog(ctx, "my-app", "dep_log")
	if err != nil || log.Content != "after contention\n" {
		t.Fatalf("persisted log = %q, error = %v", log.Content, err)
	}
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	for _, truncated := range []bool{false, true} {
		t.Run(fmt.Sprintf("truncated=%t", truncated), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			db := newTestStore(t, ctx)
			now := time.Now()
			if err := createDeploymentLog(ctx, db.db, app.Deployment{ID: "dep_log", AppID: "my-app", StartedAt: now}); err != nil {
				t.Fatal(err)
			}
			want := "after contention\n"
			if truncated {
				want = "retained"
				if _, err := db.db.ExecContext(ctx, `update deployment_logs set content = ?, truncated = 1 where deployment_id = ?`, want, "dep_log"); err != nil {
					t.Fatal(err)
				}
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
			if err != nil || log.Content != want || log.Truncated != truncated {
				t.Fatalf("persisted log = %q, error = %v", log.Content, err)
			}
		})
	}
}

func TestAppendTruncatedDeploymentLogKeepsContentAndUpdatesTime(t *testing.T) {
	ctx := context.Background()
	db := newTestStore(t, ctx)
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := createDeploymentLog(ctx, db.db, app.Deployment{ID: "dep_log", AppID: "my-app", StartedAt: started}); err != nil {
		t.Fatal(err)
	}
	if err := db.AppendDeploymentLog(ctx, "my-app", "dep_log", strings.Repeat("x", deploymentLogLimitBytes), started); err != nil {
		t.Fatal(err)
	}
	before, err := db.DeploymentLog(ctx, "my-app", "dep_log")
	if err != nil || !before.Truncated || !strings.HasSuffix(before.Content, deploymentLogTruncationMarker) {
		t.Fatalf("truncated log = %t, error = %v", before.Truncated, err)
	}
	if _, err := db.db.ExecContext(ctx, `create trigger reject_content_rewrite before update of content on deployment_logs begin select raise(abort, 'content was rewritten'); end`); err != nil {
		t.Fatal(err)
	}
	updated := started.Add(time.Minute)
	if err := db.AppendDeploymentLog(ctx, "other-app", "dep_log", "discarded", updated); !errors.Is(err, ErrNotFound) {
		t.Fatalf("append for another app = %v, want not found", err)
	}
	if err := db.AppendDeploymentLog(ctx, "my-app", "dep_log", "discarded", updated); err != nil {
		t.Fatal(err)
	}
	after, err := db.DeploymentLog(ctx, "my-app", "dep_log")
	if err != nil || after.Content != before.Content || !after.Truncated || !after.UpdatedAt.Equal(updated) {
		t.Fatalf("truncated log changed unexpectedly: truncated=%t updated=%v error=%v", after.Truncated, after.UpdatedAt, err)
	}
}

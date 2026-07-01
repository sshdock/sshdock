package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
)

func TestSQLiteStoreApps(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	model := app.App{
		ID:           "app_1",
		Name:         "my-app",
		NodeID:       "local",
		RepoPath:     "/data/apps/my-app/repo.git",
		WorktreePath: "/data/apps/my-app/worktree",
		ComposePath:  "compose.yml",
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := store.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	got, err := store.GetApp(ctx, model.ID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got != model {
		t.Fatalf("GetApp = %#v, want %#v", got, model)
	}

	apps, err := store.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0] != model {
		t.Fatalf("ListApps = %#v, want [%#v]", apps, model)
	}
}

func TestSQLiteStoreReleases(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	release := app.Release{
		ID:          "rel_1",
		AppID:       "app_1",
		CommitSHA:   "abc123",
		ComposePath: "compose.yml",
		Status:      app.ReleaseStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateRelease(ctx, release); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	got, err := store.GetRelease(ctx, release.ID)
	if err != nil {
		t.Fatalf("GetRelease: %v", err)
	}
	if got != release {
		t.Fatalf("GetRelease = %#v, want %#v", got, release)
	}

	releases, err := store.ListReleasesByApp(ctx, release.AppID)
	if err != nil {
		t.Fatalf("ListReleasesByApp: %v", err)
	}
	if len(releases) != 1 || releases[0] != release {
		t.Fatalf("ListReleasesByApp = %#v, want [%#v]", releases, release)
	}
}

func TestSQLiteStoreDeploymentStatus(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	startedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)
	deployment := app.Deployment{
		ID:        "dep_1",
		AppID:     "app_1",
		ReleaseID: "rel_1",
		Status:    app.DeploymentStatusDeploying,
		StartedAt: startedAt,
	}

	if err := store.CreateDeployment(ctx, deployment); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if err := store.UpdateDeploymentStatus(ctx, deployment.ID, app.DeploymentStatusFailed, finishedAt, "compose failed"); err != nil {
		t.Fatalf("UpdateDeploymentStatus: %v", err)
	}

	var status string
	var finishedAtText string
	var errorMessage string
	err := store.db.QueryRowContext(ctx, `select status, finished_at, error_message from deployments where id = ?`, deployment.ID).
		Scan(&status, &finishedAtText, &errorMessage)
	if err != nil {
		t.Fatalf("query deployment: %v", err)
	}
	if status != string(app.DeploymentStatusFailed) {
		t.Fatalf("status = %q", status)
	}
	if finishedAtText != formatTime(finishedAt) {
		t.Fatalf("finished_at = %q", finishedAtText)
	}
	if errorMessage != "compose failed" {
		t.Fatalf("error_message = %q", errorMessage)
	}
}

func TestSQLiteStoreDomains(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	domain := app.Domain{
		ID:          "dom_1",
		AppID:       "app_1",
		ServiceName: "web",
		DomainName:  "example.com",
		Port:        3000,
		HTTPS:       true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}

	domains, err := store.ListDomainsByApp(ctx, domain.AppID)
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 1 || domains[0] != domain {
		t.Fatalf("ListDomainsByApp = %#v, want [%#v]", domains, domain)
	}
}

func TestSQLiteStoreEvents(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	event := app.Event{
		ID:        "evt_1",
		AppID:     "app_1",
		Type:      "app.created",
		Message:   "App created",
		CreatedAt: now,
	}

	if err := store.CreateEvent(ctx, event); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	events, err := store.ListEventsByApp(ctx, event.AppID)
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if len(events) != 1 || events[0] != event {
		t.Fatalf("ListEventsByApp = %#v, want [%#v]", events, event)
	}
}

func TestSQLiteStoreMissingRowsReturnNotFound(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)

	_, err := store.GetApp(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetApp error = %v, want ErrNotFound", err)
	}

	_, err = store.GetRelease(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRelease error = %v, want ErrNotFound", err)
	}
}

func newTestStore(t *testing.T, ctx context.Context) *SQLiteStore {
	t.Helper()

	store, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "rhumbase.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	return store
}

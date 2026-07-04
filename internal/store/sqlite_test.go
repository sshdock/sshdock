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

	updatedAt := now.Add(time.Minute)
	if err := store.UpdateAppStatus(ctx, model.ID, app.AppStatusDeploying, updatedAt); err != nil {
		t.Fatalf("UpdateAppStatus: %v", err)
	}
	got, err = store.GetApp(ctx, model.ID)
	if err != nil {
		t.Fatalf("GetApp after status update: %v", err)
	}
	if got.Status != app.AppStatusDeploying {
		t.Fatalf("status after update = %q", got.Status)
	}
	if !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("updated_at after update = %s, want %s", got.UpdatedAt, updatedAt)
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

	updatedAt := now.Add(time.Minute)
	if err := store.UpdateReleaseStatus(ctx, release.ID, app.ReleaseStatusDeploying, updatedAt); err != nil {
		t.Fatalf("UpdateReleaseStatus: %v", err)
	}
	got, err = store.GetRelease(ctx, release.ID)
	if err != nil {
		t.Fatalf("GetRelease after status update: %v", err)
	}
	if got.Status != app.ReleaseStatusDeploying {
		t.Fatalf("status after update = %q", got.Status)
	}
	if !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("updated_at after update = %s, want %s", got.UpdatedAt, updatedAt)
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

	second := app.Deployment{
		ID:        "dep_2",
		AppID:     "app_1",
		ReleaseID: "rel_2",
		Status:    app.DeploymentStatusSucceeded,
		StartedAt: startedAt.Add(2 * time.Minute),
	}
	if err := store.CreateDeployment(ctx, second); err != nil {
		t.Fatalf("CreateDeployment second: %v", err)
	}
	otherApp := app.Deployment{
		ID:        "dep_other",
		AppID:     "app_2",
		ReleaseID: "rel_other",
		Status:    app.DeploymentStatusSucceeded,
		StartedAt: startedAt.Add(3 * time.Minute),
	}
	if err := store.CreateDeployment(ctx, otherApp); err != nil {
		t.Fatalf("CreateDeployment other app: %v", err)
	}

	deployments, err := store.ListDeploymentsByApp(ctx, "app_1")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 2 {
		t.Fatalf("deployments = %#v, want two app_1 rows", deployments)
	}
	if deployments[0].ID != "dep_1" || deployments[0].Status != app.DeploymentStatusFailed || deployments[0].ErrorMessage != "compose failed" {
		t.Fatalf("first deployment = %#v", deployments[0])
	}
	if deployments[1] != second {
		t.Fatalf("second deployment = %#v, want %#v", deployments[1], second)
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

	secondDomain := app.Domain{
		ID:          "dom_2",
		AppID:       "app_2",
		ServiceName: "api",
		DomainName:  "api.example.com",
		Port:        4000,
		HTTPS:       true,
		CreatedAt:   now.Add(time.Minute),
		UpdatedAt:   now.Add(time.Minute),
	}
	if err := store.AttachDomain(ctx, secondDomain); err != nil {
		t.Fatalf("AttachDomain second: %v", err)
	}

	allDomains, err := store.ListDomains(ctx)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	wantDomains := []app.Domain{domain, secondDomain}
	if len(allDomains) != len(wantDomains) {
		t.Fatalf("ListDomains len = %d, want %d: %#v", len(allDomains), len(wantDomains), allDomains)
	}
	for i := range wantDomains {
		if allDomains[i] != wantDomains[i] {
			t.Fatalf("ListDomains[%d] = %#v, want %#v", i, allDomains[i], wantDomains[i])
		}
	}

	deleted, err := store.DeleteDomainByAppAndName(ctx, domain.AppID, domain.DomainName)
	if err != nil {
		t.Fatalf("DeleteDomainByAppAndName: %v", err)
	}
	if deleted != domain {
		t.Fatalf("deleted domain = %#v, want %#v", deleted, domain)
	}
	domains, err = store.ListDomainsByApp(ctx, domain.AppID)
	if err != nil {
		t.Fatalf("ListDomainsByApp after delete: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("domains after delete = %#v, want none", domains)
	}
	if _, err := store.DeleteDomainByAppAndName(ctx, domain.AppID, domain.DomainName); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteDomainByAppAndName missing error = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreAttachDomainUpsertsByDomainID(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	createdAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	domain := app.Domain{
		ID:          "dom_my_app_my_app_example_com",
		AppID:       "my-app",
		ServiceName: "web",
		DomainName:  "my-app.example.com",
		Port:        3000,
		HTTPS:       true,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	if err := store.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain initial: %v", err)
	}

	domain.ServiceName = "api"
	domain.Port = 4000
	domain.CreatedAt = updatedAt
	domain.UpdatedAt = updatedAt
	if err := store.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain update: %v", err)
	}

	domains, err := store.ListDomainsByApp(ctx, domain.AppID)
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("domains len = %d, want 1: %#v", len(domains), domains)
	}
	got := domains[0]
	if got.ServiceName != "api" || got.Port != 4000 {
		t.Fatalf("updated domain = %#v, want api:4000", got)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %s, want original %s", got.CreatedAt, createdAt)
	}
	if !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("UpdatedAt = %s, want %s", got.UpdatedAt, updatedAt)
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

func TestSQLiteStoreServerConfig(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	updatedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

	if _, err := store.GetServerConfig(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetServerConfig error = %v, want ErrNotFound", err)
	}
	if err := store.SetServerConfig(ctx, ServerConfig{BaseDomain: "example.com", GitHost: "rhumbase.example.com", UpdatedAt: updatedAt}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}

	got, err := store.GetServerConfig(ctx)
	if err != nil {
		t.Fatalf("GetServerConfig: %v", err)
	}
	want := ServerConfig{BaseDomain: "example.com", GitHost: "rhumbase.example.com", UpdatedAt: updatedAt}
	if got != want {
		t.Fatalf("server config = %#v, want %#v", got, want)
	}
}

func TestSQLiteStoreServerConfigKeepsLegacyGitHost(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	updatedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

	if err := store.SetServerConfig(ctx, ServerConfig{GitHost: "rhumbase.example.com", UpdatedAt: updatedAt}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}

	got, err := store.GetServerConfig(ctx)
	if err != nil {
		t.Fatalf("GetServerConfig: %v", err)
	}
	want := ServerConfig{GitHost: "rhumbase.example.com", UpdatedAt: updatedAt}
	if got != want {
		t.Fatalf("server config = %#v, want %#v", got, want)
	}
}

func TestSQLiteStoreSSHKeys(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	createdAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	key := SSHKey{
		Name:      "admin",
		PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com",
		CreatedAt: createdAt,
	}

	if err := store.UpsertSSHKey(ctx, key); err != nil {
		t.Fatalf("UpsertSSHKey: %v", err)
	}
	keys, err := store.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("ListSSHKeys: %v", err)
	}
	if len(keys) != 1 || keys[0] != key {
		t.Fatalf("keys = %#v, want [%#v]", keys, key)
	}

	replacement := SSHKey{
		Name:      "admin",
		PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIReplacement admin@example.com",
		CreatedAt: createdAt.Add(time.Minute),
	}
	if err := store.UpsertSSHKey(ctx, replacement); err != nil {
		t.Fatalf("UpsertSSHKey replacement: %v", err)
	}
	keys, err = store.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("ListSSHKeys replacement: %v", err)
	}
	if len(keys) != 1 || keys[0] != replacement {
		t.Fatalf("keys after replacement = %#v, want [%#v]", keys, replacement)
	}

	if err := store.DeleteSSHKey(ctx, "admin"); err != nil {
		t.Fatalf("DeleteSSHKey: %v", err)
	}
	keys, err = store.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("ListSSHKeys after delete: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("keys after delete = %#v, want none", keys)
	}
	if err := store.DeleteSSHKey(ctx, "admin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteSSHKey missing error = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreDeleteAppRemovesRelatedRows(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	model := app.App{
		ID:           "app_1",
		Name:         "my-app",
		NodeID:       "local",
		RepoPath:     "/data/apps/my-app/repo.git",
		WorktreePath: "/data/apps/my-app/worktree",
		Status:       app.AppStatusHealthy,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := store.CreateRelease(ctx, app.Release{ID: "rel_1", AppID: model.ID, CommitSHA: "abc123", ComposePath: "compose.yml", Status: app.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	if err := store.CreateDeployment(ctx, app.Deployment{ID: "dep_1", AppID: model.ID, ReleaseID: "rel_1", Status: app.DeploymentStatusSucceeded, StartedAt: now, FinishedAt: now}); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if err := store.AttachDomain(ctx, app.Domain{ID: "dom_1", AppID: model.ID, ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	if err := store.CreateEvent(ctx, app.Event{ID: "evt_1", AppID: model.ID, Type: "deploy.succeeded", Message: "Deploy succeeded", CreatedAt: now}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	if err := store.DeleteApp(ctx, model.ID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}

	if _, err := store.GetApp(ctx, model.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetApp after DeleteApp error = %v, want ErrNotFound", err)
	}
	if releases, err := store.ListReleasesByApp(ctx, model.ID); err != nil || len(releases) != 0 {
		t.Fatalf("releases after DeleteApp = %#v, err = %v", releases, err)
	}
	if deployments, err := store.ListDeploymentsByApp(ctx, model.ID); err != nil || len(deployments) != 0 {
		t.Fatalf("deployments after DeleteApp = %#v, err = %v", deployments, err)
	}
	if domains, err := store.ListDomainsByApp(ctx, model.ID); err != nil || len(domains) != 0 {
		t.Fatalf("domains after DeleteApp = %#v, err = %v", domains, err)
	}
	if events, err := store.ListEventsByApp(ctx, model.ID); err != nil || len(events) != 0 {
		t.Fatalf("events after DeleteApp = %#v, err = %v", events, err)
	}
	if err := store.DeleteApp(ctx, model.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteApp missing error = %v, want ErrNotFound", err)
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

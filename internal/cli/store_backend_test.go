package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
	"github.com/iketiunn/rumbase/internal/gitrecv"
	"github.com/iketiunn/rumbase/internal/store"
)

func TestStoreBackendCreatesReceiveRepoWhenAppIsCreated(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	setupper := &fakeReceiveRepoSetupper{
		repo: gitrecv.BareRepo{
			Path:      filepath.Join(appsDir, "my-app", "repo.git"),
			RemoteURL: "git@git.example.com:my-app.git",
		},
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:       "node-a",
		AppsDir:      appsDir,
		RepoSetupper: setupper,
	})
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runner.Run([]string{"apps", "create", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps create exit code = %d, stderr = %q", code, stderr.String())
	}

	if len(setupper.apps) != 1 || setupper.apps[0] != "my-app" {
		t.Fatalf("setupper apps = %#v, want [my-app]", setupper.apps)
	}
	if !strings.Contains(stdout.String(), "git remote add rhumbase git@git.example.com:my-app.git") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	model, err := sqlite.GetApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if model.RepoPath != setupper.repo.Path {
		t.Fatalf("repo path = %q, want %q", model.RepoPath, setupper.repo.Path)
	}
}

func TestStoreBackendDoesNotPersistAppWhenReceiveRepoSetupFails(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	setupper := &fakeReceiveRepoSetupper{err: errors.New("git init failed")}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:       "node-a",
		AppsDir:      filepath.Join(t.TempDir(), "apps"),
		RepoSetupper: setupper,
	})
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runner.Run([]string{"apps", "create", "my-app"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("apps create exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "set up receive repo") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	_, err := sqlite.GetApp(ctx, "my-app")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetApp error = %v, want ErrNotFound", err)
	}
}

type fakeReceiveRepoSetupper struct {
	repo gitrecv.BareRepo
	err  error
	apps []string
}

func (f *fakeReceiveRepoSetupper) SetupBareRepo(_ context.Context, appName string) (gitrecv.BareRepo, error) {
	f.apps = append(f.apps, appName)
	if f.err != nil {
		return gitrecv.BareRepo{}, f.err
	}

	return f.repo, nil
}

func TestStoreBackendPersistsAppsAndDomains(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:  "node-a",
		AppsDir: appsDir,
		GitHost: "git.example.com",
		Now:     func() time.Time { return now },
	})
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runner.Run([]string{"apps", "create", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps create exit code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"created app my-app",
		"git remote add rhumbase git@git.example.com:my-app.git",
		"git push rhumbase main",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("apps create stdout missing %q:\n%s", want, stdout.String())
		}
	}

	model, err := sqlite.GetApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	wantApp := app.App{
		ID:           "my-app",
		Name:         "my-app",
		NodeID:       "node-a",
		RepoPath:     filepath.Join(appsDir, "my-app", "repo.git"),
		WorktreePath: filepath.Join(appsDir, "my-app", "worktree"),
		ComposePath:  "",
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if model != wantApp {
		t.Fatalf("stored app = %#v, want %#v", model, wantApp)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"apps", "list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps list exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "my-app\tcreated\tnode-a") {
		t.Fatalf("apps list stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"domains", "attach", "my-app", "web", "example.com", "--port", "3000"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains attach exit code = %d, stderr = %q", code, stderr.String())
	}

	domains, err := sqlite.ListDomainsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("domains len = %d, want 1", len(domains))
	}
	wantDomain := app.Domain{
		ID:          "dom_my_app_example_com",
		AppID:       "my-app",
		ServiceName: "web",
		DomainName:  "example.com",
		Port:        3000,
		HTTPS:       true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if domains[0] != wantDomain {
		t.Fatalf("stored domain = %#v, want %#v", domains[0], wantDomain)
	}
}

func newStoreBackendTestStore(t *testing.T, ctx context.Context) *store.SQLiteStore {
	t.Helper()

	sqlite, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "rhumbase.db"))
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

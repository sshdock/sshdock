package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
	"github.com/iketiunn/rumbase/internal/store"
)

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
		"git remote add prod git@git.example.com:my-app",
		"git push prod main",
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

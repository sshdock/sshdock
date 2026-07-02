package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
	"github.com/iketiunn/rumbase/internal/gitrecv"
	"github.com/iketiunn/rumbase/internal/router"
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

type fakeRoutePublisher struct {
	Syncs [][]router.Route
	Err   error
}

func (f *fakeRoutePublisher) SyncRoutes(_ context.Context, routes []router.Route) error {
	copied := append([]router.Route(nil), routes...)
	f.Syncs = append(f.Syncs, copied)
	return f.Err
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

func TestStoreBackendDomainAttachRebuildsRouterFromPersistedDomainsAndRecordsEvents(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	routePublisher := &fakeRoutePublisher{}
	currentTime := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:  "node-a",
		AppsDir: filepath.Join(t.TempDir(), "apps"),
		Router:  routePublisher,
		Now: func() time.Time {
			value := currentTime
			currentTime = currentTime.Add(time.Minute)
			return value
		},
	})
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	for _, appName := range []string{"my-app", "api-app"} {
		stdout.Reset()
		stderr.Reset()
		if code := runner.Run([]string{"apps", "create", appName}, &stdout, &stderr); code != 0 {
			t.Fatalf("apps create %s exit code = %d, stderr = %q", appName, code, stderr.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"domains", "attach", "my-app", "web", "www.example.com", "--port", "3000"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains attach first exit code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"domains", "attach", "api-app", "api", "api.example.com", "--port", "4000"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains attach second exit code = %d, stderr = %q", code, stderr.String())
	}

	if len(routePublisher.Syncs) != 2 {
		t.Fatalf("router sync count = %d, want 2: %#v", len(routePublisher.Syncs), routePublisher.Syncs)
	}
	wantLastSync := []router.Route{
		{AppID: "my-app", ServiceName: "web", DomainName: "www.example.com", Port: 3000, HTTPS: true},
		{AppID: "api-app", ServiceName: "api", DomainName: "api.example.com", Port: 4000, HTTPS: true},
	}
	gotLastSync := routePublisher.Syncs[len(routePublisher.Syncs)-1]
	if len(gotLastSync) != len(wantLastSync) {
		t.Fatalf("last router sync len = %d, want %d: %#v", len(gotLastSync), len(wantLastSync), gotLastSync)
	}
	for i := range wantLastSync {
		if gotLastSync[i] != wantLastSync[i] {
			t.Fatalf("last router sync[%d] = %#v, want %#v", i, gotLastSync[i], wantLastSync[i])
		}
	}

	events, err := sqlite.ListEventsByApp(ctx, "api-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	gotTypes := make([]string, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event.Type)
	}
	if strings.Join(gotTypes, ",") != "domain.attached,router.reloaded" {
		t.Fatalf("event types = %#v, want domain.attached and router.reloaded", gotTypes)
	}
}

func TestStoreBackendUsesPersistedServerGitHostForAppRemotes(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:  "node-a",
		AppsDir: filepath.Join(t.TempDir(), "apps"),
		GitHost: "env.example.com",
	})
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runner.Run([]string{"server", "domain", "set", "rhumbase.example.com"}, &stdout, &stderr); code != 0 {
		t.Fatalf("server domain set exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "server Git host set to rhumbase.example.com") {
		t.Fatalf("server domain set stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"apps", "create", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps create exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "git remote add rhumbase git@rhumbase.example.com:my-app.git") {
		t.Fatalf("apps create stdout = %q", stdout.String())
	}
}

func TestStoreBackendAddsSSHKeyAndRendersAuthorizedKeys(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	authorizedKeysPath := filepath.Join(t.TempDir(), "git", ".ssh", "authorized_keys")
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:             "node-a",
		AppsDir:            filepath.Join(t.TempDir(), "apps"),
		AuthorizedKeysPath: authorizedKeysPath,
		GitReceiveCommand:  "/usr/local/bin/rhumbased git-receive",
		Now:                func() time.Time { return now },
	})
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	publicKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com\n"

	code := runner.RunWithInput([]string{"ssh-keys", "add", "admin"}, strings.NewReader(publicKey), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("ssh-keys add exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "added SSH key admin") {
		t.Fatalf("ssh-keys add stdout = %q", stdout.String())
	}

	keys, err := sqlite.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("ListSSHKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("keys = %#v", keys)
	}
	if keys[0].Name != "admin" || keys[0].PublicKey != strings.TrimSpace(publicKey) || !keys[0].CreatedAt.Equal(now) {
		t.Fatalf("stored key = %#v", keys[0])
	}

	rendered, err := os.ReadFile(authorizedKeysPath)
	if err != nil {
		t.Fatalf("ReadFile authorized_keys: %v", err)
	}
	for _, want := range []string{
		`command="exec /usr/local/bin/rhumbased git-receive"`,
		`no-pty`,
		`no-port-forwarding`,
		`no-agent-forwarding`,
		`no-X11-forwarding`,
		strings.TrimSpace(publicKey),
	} {
		if !strings.Contains(string(rendered), want) {
			t.Fatalf("authorized_keys missing %q:\n%s", want, rendered)
		}
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

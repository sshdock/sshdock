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
	"github.com/iketiunn/rumbase/internal/compose"
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
	dashboardAuthorizedKeysPath := filepath.Join(t.TempDir(), "dashboard", ".ssh", "authorized_keys")
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:                      "node-a",
		AppsDir:                     filepath.Join(t.TempDir(), "apps"),
		AuthorizedKeysPath:          authorizedKeysPath,
		GitReceiveCommand:           "/usr/local/bin/rhumbased git-receive",
		DashboardAuthorizedKeysPath: dashboardAuthorizedKeysPath,
		DashboardCommand:            "/usr/local/bin/rhumbased dashboard",
		Now:                         func() time.Time { return now },
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

	renderedGit, err := os.ReadFile(authorizedKeysPath)
	if err != nil {
		t.Fatalf("ReadFile git authorized_keys: %v", err)
	}
	for _, want := range []string{
		`command="exec /usr/local/bin/rhumbased git-receive"`,
		`no-pty`,
		`no-port-forwarding`,
		`no-agent-forwarding`,
		`no-X11-forwarding`,
		strings.TrimSpace(publicKey),
	} {
		if !strings.Contains(string(renderedGit), want) {
			t.Fatalf("git authorized_keys missing %q:\n%s", want, renderedGit)
		}
	}

	renderedDashboard, err := os.ReadFile(dashboardAuthorizedKeysPath)
	if err != nil {
		t.Fatalf("ReadFile dashboard authorized_keys: %v", err)
	}
	for _, want := range []string{
		`command="exec /usr/local/bin/rhumbased dashboard"`,
		`no-port-forwarding`,
		`no-agent-forwarding`,
		`no-X11-forwarding`,
		strings.TrimSpace(publicKey),
	} {
		if !strings.Contains(string(renderedDashboard), want) {
			t.Fatalf("dashboard authorized_keys missing %q:\n%s", want, renderedDashboard)
		}
	}
	if strings.Contains(string(renderedDashboard), "no-pty") {
		t.Fatalf("dashboard authorized_keys should allow PTY:\n%s", renderedDashboard)
	}
}

func TestStoreBackendLifecycleInspectionCommands(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	runner := &compose.FakeRunner{LogOutput: "web log\n"}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:         "node-a",
		AppsDir:        appsDir,
		RecoveryRunner: runner,
		Now:            func() time.Time { return now },
	})
	cliRunner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	if err := sqlite.AttachDomain(ctx, app.Domain{ID: "dom_my_app_example_com", AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	if err := sqlite.CreateEvent(ctx, app.Event{ID: "evt_1", AppID: "my-app", Type: "deploy.succeeded", Message: "Deploy succeeded", CreatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if err := sqlite.UpsertSSHKey(ctx, store.SSHKey{Name: "admin", PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com", CreatedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("UpsertSSHKey: %v", err)
	}

	if code := cliRunner.Run([]string{"logs", "my-app", "web", "-f"}, &stdout, &stderr); code != 0 {
		t.Fatalf("logs exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "web log\n" {
		t.Fatalf("logs stdout = %q", stdout.String())
	}
	if len(runner.LogsRequests) != 1 {
		t.Fatalf("LogsRequests = %#v", runner.LogsRequests)
	}
	logsRequest := runner.LogsRequests[0]
	if logsRequest.AppName != "my-app" || logsRequest.ServiceName != "web" || logsRequest.ProjectDir != filepath.Join(appsDir, "my-app", "worktree") || logsRequest.ComposePath != filepath.Join(appsDir, "my-app", "worktree", "compose.yml") || logsRequest.Lines != 100 || !logsRequest.Follow {
		t.Fatalf("logs request = %#v", logsRequest)
	}

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "releases list", args: []string{"releases", "list", "my-app"}, want: []string{"rel_old\tsucceeded\told\t", "rel_new\tsucceeded\tnew\t"}},
		{name: "events list", args: []string{"events", "list", "my-app"}, want: []string{"deploy.succeeded\tDeploy succeeded"}},
		{name: "domains list", args: []string{"domains", "list", "my-app"}, want: []string{"example.com\tweb\t3000\ttrue"}},
		{name: "ssh-keys list", args: []string{"ssh-keys", "list"}, want: []string{"admin\t", "\t2026-07-04T10:02:00Z"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			if code := cliRunner.Run(test.args, &stdout, &stderr); code != 0 {
				t.Fatalf("%s exit code = %d, stderr = %q", test.name, code, stderr.String())
			}
			for _, want := range test.want {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("%s stdout missing %q:\n%s", test.name, want, stdout.String())
				}
			}
		})
	}
}

func TestStoreBackendDomainDetachDeletesDomainRebuildsRoutesAndRecordsEvents(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	routePublisher := &fakeRoutePublisher{}
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:  "node-a",
		AppsDir: filepath.Join(t.TempDir(), "apps"),
		Router:  routePublisher,
		Now:     func() time.Time { return now },
	})
	cliRunner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	for _, model := range []app.App{
		{ID: "my-app", Name: "my-app", NodeID: "node-a", Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now},
		{ID: "api-app", Name: "api-app", NodeID: "node-a", Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now},
	} {
		model.RepoPath = filepath.Join(t.TempDir(), model.ID, "repo.git")
		model.WorktreePath = filepath.Join(t.TempDir(), model.ID, "worktree")
		if err := sqlite.CreateApp(ctx, model); err != nil {
			t.Fatalf("CreateApp %s: %v", model.ID, err)
		}
	}
	for _, domain := range []app.Domain{
		{ID: "dom_my_app_example_com", AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now},
		{ID: "dom_api_app_api_example_com", AppID: "api-app", ServiceName: "api", DomainName: "api.example.com", Port: 4000, HTTPS: true, CreatedAt: now, UpdatedAt: now},
	} {
		if err := sqlite.AttachDomain(ctx, domain); err != nil {
			t.Fatalf("AttachDomain %s: %v", domain.DomainName, err)
		}
	}

	if code := cliRunner.Run([]string{"domains", "detach", "my-app", "example.com"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains detach exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "detached example.com from my-app") {
		t.Fatalf("domains detach stdout = %q", stdout.String())
	}
	domains, err := sqlite.ListDomainsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("my-app domains after detach = %#v", domains)
	}
	if len(routePublisher.Syncs) != 1 {
		t.Fatalf("router syncs = %#v", routePublisher.Syncs)
	}
	wantRoutes := []router.Route{{AppID: "api-app", ServiceName: "api", DomainName: "api.example.com", Port: 4000, HTTPS: true}}
	if len(routePublisher.Syncs[0]) != len(wantRoutes) || routePublisher.Syncs[0][0] != wantRoutes[0] {
		t.Fatalf("router sync = %#v, want %#v", routePublisher.Syncs[0], wantRoutes)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	gotTypes := make([]string, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event.Type)
	}
	if strings.Join(gotTypes, ",") != "domain.detached,router.reloaded" {
		t.Fatalf("event types = %#v, want domain.detached and router.reloaded", gotTypes)
	}
}

func TestStoreBackendSSHKeyRemoveRevokesKeyAndRendersAuthorizedKeys(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	authorizedKeysPath := filepath.Join(t.TempDir(), "git", ".ssh", "authorized_keys")
	dashboardAuthorizedKeysPath := filepath.Join(t.TempDir(), "dashboard", ".ssh", "authorized_keys")
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:                      "node-a",
		AppsDir:                     filepath.Join(t.TempDir(), "apps"),
		AuthorizedKeysPath:          authorizedKeysPath,
		GitReceiveCommand:           "/usr/local/bin/rhumbased git-receive",
		DashboardAuthorizedKeysPath: dashboardAuthorizedKeysPath,
		DashboardCommand:            "/usr/local/bin/rhumbased dashboard",
	})
	cliRunner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	adminKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAdminKey admin@example.com\n"
	opsKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOpsKey ops@example.com\n"

	for name, key := range map[string]string{"admin": adminKey, "ops": opsKey} {
		stdout.Reset()
		stderr.Reset()
		if code := cliRunner.RunWithInput([]string{"ssh-keys", "add", name}, strings.NewReader(key), &stdout, &stderr); code != 0 {
			t.Fatalf("ssh-keys add %s exit code = %d, stderr = %q", name, code, stderr.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := cliRunner.Run([]string{"ssh-keys", "remove", "admin"}, &stdout, &stderr); code != 0 {
		t.Fatalf("ssh-keys remove exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "removed SSH key admin") {
		t.Fatalf("ssh-keys remove stdout = %q", stdout.String())
	}
	keys, err := sqlite.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("ListSSHKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].Name != "ops" {
		t.Fatalf("keys after remove = %#v, want only ops", keys)
	}
	renderedGit := readTextFile(t, authorizedKeysPath)
	if strings.Contains(renderedGit, strings.TrimSpace(adminKey)) {
		t.Fatalf("git authorized_keys still contains removed key:\n%s", renderedGit)
	}
	if !strings.Contains(renderedGit, strings.TrimSpace(opsKey)) {
		t.Fatalf("git authorized_keys missing remaining key:\n%s", renderedGit)
	}
	renderedDashboard := readTextFile(t, dashboardAuthorizedKeysPath)
	if strings.Contains(renderedDashboard, strings.TrimSpace(adminKey)) {
		t.Fatalf("dashboard authorized_keys still contains removed key:\n%s", renderedDashboard)
	}
	if !strings.Contains(renderedDashboard, strings.TrimSpace(opsKey)) {
		t.Fatalf("dashboard authorized_keys missing remaining key:\n%s", renderedDashboard)
	}
}

func TestStoreBackendAppRemoveCleansRuntimeStateAndPreservesOtherRoutes(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	runner := &compose.FakeRunner{}
	routePublisher := &fakeRoutePublisher{}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:         "node-a",
		AppsDir:        appsDir,
		RecoveryRunner: runner,
		Router:         routePublisher,
		Now:            func() time.Time { return now },
	})
	cliRunner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	appRoot := filepath.Join(appsDir, "my-app")
	repoPath := filepath.Join(appRoot, "repo.git")
	worktreePath := filepath.Join(appRoot, "worktree")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll worktree: %v", err)
	}
	composePath := filepath.Join(worktreePath, "compose.yml")
	if err := os.WriteFile(composePath, []byte("services:\n  web:\n    image: example/web:latest\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	model := app.App{ID: "my-app", Name: "my-app", NodeID: "node-a", RepoPath: repoPath, WorktreePath: worktreePath, Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}
	if err := sqlite.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp my-app: %v", err)
	}
	if err := sqlite.CreateRelease(ctx, app.Release{ID: "rel_1", AppID: "my-app", CommitSHA: "abc123", ComposePath: composePath, Status: app.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	if err := sqlite.CreateDeployment(ctx, app.Deployment{ID: "dep_1", AppID: "my-app", ReleaseID: "rel_1", Status: app.DeploymentStatusSucceeded, StartedAt: now, FinishedAt: now}); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if err := sqlite.AttachDomain(ctx, app.Domain{ID: "dom_my_app_example_com", AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("AttachDomain my-app: %v", err)
	}
	if err := sqlite.CreateEvent(ctx, app.Event{ID: "evt_1", AppID: "my-app", Type: "deploy.succeeded", Message: "Deploy succeeded", CreatedAt: now}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	other := app.App{ID: "api-app", Name: "api-app", NodeID: "node-a", RepoPath: filepath.Join(appsDir, "api-app", "repo.git"), WorktreePath: filepath.Join(appsDir, "api-app", "worktree"), Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}
	if err := sqlite.CreateApp(ctx, other); err != nil {
		t.Fatalf("CreateApp api-app: %v", err)
	}
	if err := sqlite.AttachDomain(ctx, app.Domain{ID: "dom_api_app_api_example_com", AppID: "api-app", ServiceName: "api", DomainName: "api.example.com", Port: 4000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("AttachDomain api-app: %v", err)
	}

	if code := cliRunner.Run([]string{"apps", "remove", "my-app", "--force"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps remove exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "removed app my-app") {
		t.Fatalf("apps remove stdout = %q", stdout.String())
	}
	if len(runner.RemoveRequests) != 1 {
		t.Fatalf("RemoveRequests = %#v", runner.RemoveRequests)
	}
	removeRequest := runner.RemoveRequests[0]
	if removeRequest.AppName != "my-app" || removeRequest.ProjectDir != worktreePath || removeRequest.ComposePath != composePath {
		t.Fatalf("remove request = %#v", removeRequest)
	}
	if _, err := os.Stat(repoPath); !os.IsNotExist(err) {
		t.Fatalf("repo path stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree path stat err = %v, want not exist", err)
	}
	if _, err := sqlite.GetApp(ctx, "my-app"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetApp after remove error = %v, want ErrNotFound", err)
	}
	if releases, err := sqlite.ListReleasesByApp(ctx, "my-app"); err != nil || len(releases) != 0 {
		t.Fatalf("releases after remove = %#v, err = %v", releases, err)
	}
	if deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app"); err != nil || len(deployments) != 0 {
		t.Fatalf("deployments after remove = %#v, err = %v", deployments, err)
	}
	if domains, err := sqlite.ListDomainsByApp(ctx, "my-app"); err != nil || len(domains) != 0 {
		t.Fatalf("domains after remove = %#v, err = %v", domains, err)
	}
	if events, err := sqlite.ListEventsByApp(ctx, "my-app"); err != nil || len(events) != 0 {
		t.Fatalf("events after remove = %#v, err = %v", events, err)
	}
	if len(routePublisher.Syncs) != 1 {
		t.Fatalf("router syncs = %#v", routePublisher.Syncs)
	}
	wantRoutes := []router.Route{{AppID: "api-app", ServiceName: "api", DomainName: "api.example.com", Port: 4000, HTTPS: true}}
	if len(routePublisher.Syncs[0]) != len(wantRoutes) || routePublisher.Syncs[0][0] != wantRoutes[0] {
		t.Fatalf("router sync = %#v, want %#v", routePublisher.Syncs[0], wantRoutes)
	}
}

func TestStoreBackendRecoveryCommandsUseComposeRunnerAndRecordState(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	currentTime := now
	runner := &compose.FakeRunner{}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:         "node-a",
		AppsDir:        appsDir,
		RecoveryRunner: runner,
		Now: func() time.Time {
			value := currentTime
			currentTime = currentTime.Add(time.Second)
			return value
		},
	})
	cliRunner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	seedRecoveryApp(t, ctx, sqlite, appsDir, now)

	if code := cliRunner.Run([]string{"apps", "restart", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps restart exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "restarted app my-app") {
		t.Fatalf("apps restart stdout = %q", stdout.String())
	}
	if len(runner.RestartRequests) != 1 {
		t.Fatalf("RestartRequests = %#v", runner.RestartRequests)
	}
	appRestart := runner.RestartRequests[0]
	if appRestart.AppName != "my-app" || appRestart.ServiceName != "" || appRestart.ProjectDir != filepath.Join(appsDir, "my-app", "worktree") || appRestart.ComposePath != filepath.Join(appsDir, "my-app", "worktree", "compose.yml") {
		t.Fatalf("app restart request = %#v", appRestart)
	}

	stdout.Reset()
	stderr.Reset()
	if code := cliRunner.Run([]string{"apps", "restart", "my-app", "web"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps restart service exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "restarted my-app/web") {
		t.Fatalf("apps restart service stdout = %q", stdout.String())
	}
	if len(runner.RestartRequests) != 2 {
		t.Fatalf("RestartRequests = %#v", runner.RestartRequests)
	}
	if runner.RestartRequests[1].ServiceName != "web" {
		t.Fatalf("service restart request = %#v", runner.RestartRequests[1])
	}

	stdout.Reset()
	stderr.Reset()
	if code := cliRunner.Run([]string{"apps", "redeploy", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps redeploy exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "redeployed my-app") {
		t.Fatalf("apps redeploy stdout = %q", stdout.String())
	}
	if len(runner.DeployRequests) != 1 {
		t.Fatalf("DeployRequests = %#v", runner.DeployRequests)
	}
	if runner.DeployRequests[0].ReleaseID != "rel_new" || runner.DeployRequests[0].CommitSHA != "new" {
		t.Fatalf("redeploy request = %#v", runner.DeployRequests[0])
	}

	stdout.Reset()
	stderr.Reset()
	if code := cliRunner.Run([]string{"apps", "rollback", "my-app", "rel_old"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps rollback exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "rolled back my-app to rel_old") {
		t.Fatalf("apps rollback stdout = %q", stdout.String())
	}
	if len(runner.DeployRequests) != 2 {
		t.Fatalf("DeployRequests = %#v", runner.DeployRequests)
	}
	if runner.DeployRequests[1].ReleaseID != "rel_old" || runner.DeployRequests[1].CommitSHA != "old" {
		t.Fatalf("rollback request = %#v", runner.DeployRequests[1])
	}

	model, err := sqlite.GetApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if model.Status != app.AppStatusHealthy {
		t.Fatalf("app status = %q", model.Status)
	}
	release, err := sqlite.GetRelease(ctx, "rel_old")
	if err != nil {
		t.Fatalf("GetRelease: %v", err)
	}
	if release.Status != app.ReleaseStatusRolledBack {
		t.Fatalf("release status = %q", release.Status)
	}
	deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 2 {
		t.Fatalf("deployments = %#v", deployments)
	}
	for _, deployment := range deployments {
		if deployment.Status != app.DeploymentStatusSucceeded {
			t.Fatalf("deployment = %#v", deployment)
		}
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	gotTypes := make([]string, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event.Type)
	}
	wantTypes := "restart.triggered,restart.succeeded,service.restart.triggered,service.restart.succeeded,redeploy.started,redeploy.succeeded,rollback.triggered,rollback.succeeded"
	if strings.Join(gotTypes, ",") != wantTypes {
		t.Fatalf("event types = %#v, want %s", gotTypes, wantTypes)
	}
}

func TestStoreBackendRestartAppCanRunRepeatedly(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	currentTime := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	runner := &compose.FakeRunner{}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:         "node-a",
		AppsDir:        appsDir,
		RecoveryRunner: runner,
		Now: func() time.Time {
			value := currentTime
			currentTime = currentTime.Add(time.Second)
			return value
		},
	})
	cliRunner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	seedRecoveryApp(t, ctx, sqlite, appsDir, currentTime)

	if code := cliRunner.Run([]string{"apps", "restart", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("first apps restart exit code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := cliRunner.Run([]string{"apps", "restart", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("second apps restart exit code = %d, stderr = %q", code, stderr.String())
	}
	if len(runner.RestartRequests) != 2 {
		t.Fatalf("RestartRequests = %#v", runner.RestartRequests)
	}

	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	gotTypes := make([]string, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event.Type)
	}
	wantTypes := "restart.triggered,restart.succeeded,restart.triggered,restart.succeeded"
	if strings.Join(gotTypes, ",") != wantTypes {
		t.Fatalf("event types = %#v, want %s", gotTypes, wantTypes)
	}
}

func seedRecoveryApp(t *testing.T, ctx context.Context, sqlite *store.SQLiteStore, appsDir string, now time.Time) {
	t.Helper()

	model := app.App{
		ID:           "my-app",
		Name:         "my-app",
		NodeID:       "node-a",
		RepoPath:     filepath.Join(appsDir, "my-app", "repo.git"),
		WorktreePath: filepath.Join(appsDir, "my-app", "worktree"),
		Status:       app.AppStatusHealthy,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := sqlite.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	releases := []app.Release{
		{ID: "rel_old", AppID: "my-app", CommitSHA: "old", ComposePath: filepath.Join(model.WorktreePath, "compose.yml"), Status: app.ReleaseStatusSucceeded, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
		{ID: "rel_new", AppID: "my-app", CommitSHA: "new", ComposePath: filepath.Join(model.WorktreePath, "compose.yml"), Status: app.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now},
	}
	for _, release := range releases {
		if err := sqlite.CreateRelease(ctx, release); err != nil {
			t.Fatalf("CreateRelease: %v", err)
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

func readTextFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(data)
}

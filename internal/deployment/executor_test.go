package deployment

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

func TestExecutorCreatesReleaseAndSucceededDeployment(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	worktreePath := filepath.Join(filepath.Dir(dbPath), "worktree")
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	runner := &compose.FakeRunner{DeployResult: compose.DeployResult{
		RouteFound:  true,
		RouteTarget: compose.RouteTarget{ServiceName: "web", Port: 3000},
	}}
	handler := newTestExecutor(t, Config{
		Store:  sqlite,
		Runner: runner,
		Checkout: executorCheckoutFunc(func(_ context.Context, repoPath string, gotWorktreePath string, commitSHA string) error {
			if repoPath != "/apps/my-app/repo.git" {
				t.Fatalf("repoPath = %q", repoPath)
			}
			if gotWorktreePath != worktreePath {
				t.Fatalf("worktreePath = %q", gotWorktreePath)
			}
			if commitSHA != "abc123" {
				t.Fatalf("commitSHA = %q", commitSHA)
			}
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
		Now: func() time.Time { return now },
	})

	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	releases, err := sqlite.ListReleasesByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListReleasesByApp: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %#v", releases)
	}
	if releases[0].ID != app.ReleaseID("my-app", "abc123") || releases[0].CommitSHA != "abc123" || releases[0].ComposePath != filepath.Join(worktreePath, "compose.yml") {
		t.Fatalf("release = %#v", releases[0])
	}
	if releases[0].Status != app.ReleaseStatusSucceeded {
		t.Fatalf("release status = %q, want %q", releases[0].Status, app.ReleaseStatusSucceeded)
	}

	if len(runner.DeployRequests) != 1 {
		t.Fatalf("DeployRequests = %#v", runner.DeployRequests)
	}
	request := runner.DeployRequests[0]
	if request.AppName != "my-app" || request.ProjectDir != worktreePath || request.ReleaseID != app.ReleaseID("my-app", "abc123") || request.CommitSHA != "abc123" {
		t.Fatalf("DeployRequest = %#v", request)
	}

	status := queryDeploymentStatus(t, dbPath, "dep_abc123")
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
	errorMessage := queryDeploymentError(t, dbPath, "dep_abc123")
	if errorMessage != "" {
		t.Fatalf("deployment error = %q, want empty", errorMessage)
	}

	model, err := sqlite.GetApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if model.Status != app.AppStatusHealthy {
		t.Fatalf("app status = %q, want %q", model.Status, app.AppStatusHealthy)
	}

	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.queued", "deploy.started", "deploy.succeeded"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
}

func TestExecutorCreatesUniqueAttemptsForRepeatedCommit(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newExecutorTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	ids := []string{"dep_00000000000000000000000000000001", "dep_00000000000000000000000000000002"}
	handler := newTestExecutor(t, Config{
		Store: sqlite,
		Runner: &compose.FakeRunner{DeployResult: compose.DeployResult{
			RouteFound:  true,
			RouteTarget: compose.RouteTarget{ServiceName: "web", Port: 3000},
		}},
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	// When
	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", ids[0]); err != nil {
		t.Fatalf("Execute first: %v", err)
	}
	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", ids[1]); err != nil {
		t.Fatalf("Execute second: %v", err)
	}

	// Then
	releases, err := sqlite.ListReleasesByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListReleasesByApp: %v", err)
	}
	if len(releases) != 1 || releases[0].ID != app.ReleaseID("my-app", "abc123") {
		t.Fatalf("releases = %#v, want one stable app/commit release", releases)
	}
	deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 2 {
		t.Fatalf("deployments = %#v, want two attempts", deployments)
	}
	for index, deployment := range deployments {
		if deployment.ID != ids[index] || deployment.ReleaseID != releases[0].ID || deployment.CommitSHA != "abc123" || deployment.Trigger != app.DeploymentTriggerRedeploy {
			t.Fatalf("deployment[%d] = %#v", index, deployment)
		}
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if len(events) != 6 {
		t.Fatalf("events = %#v, want queued, started, and succeeded events per attempt", events)
	}
}

func TestExecutorScopesSameCommitReleaseAcrossApps(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newExecutorTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	if err := sqlite.CreateApp(ctx, app.App{ID: "other-app", Name: "other-app", NodeID: "local", RepoPath: "/apps/other-app/repo.git", WorktreePath: t.TempDir(), Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateApp other-app: %v", err)
	}
	ids := []string{"dep_10000000000000000000000000000001", "dep_20000000000000000000000000000002"}
	handler := newTestExecutor(t, Config{
		Store:  sqlite,
		Runner: &compose.FakeRunner{},
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	// When
	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", ids[0]); err != nil {
		t.Fatalf("Execute my-app: %v", err)
	}
	if err := executeAttempt(t, ctx, sqlite, handler, "other-app", "abc123", ids[1]); err != nil {
		t.Fatalf("Execute other-app: %v", err)
	}

	// Then
	first, err := sqlite.GetReleaseByAppCommit(ctx, "my-app", "abc123")
	if err != nil {
		t.Fatalf("GetReleaseByAppCommit my-app: %v", err)
	}
	second, err := sqlite.GetReleaseByAppCommit(ctx, "other-app", "abc123")
	if err != nil {
		t.Fatalf("GetReleaseByAppCommit other-app: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("release IDs = %q, want app-scoped identities", first.ID)
	}
}

func TestExecutorAutoRoutesAfterSuccessfulDeploy(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "sshdock.example.com",
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	routeSyncer := &fakeExecutorRouteSyncer{}
	handler := newTestExecutor(t, Config{
		Store: sqlite,
		Runner: &compose.FakeRunner{DeployResult: compose.DeployResult{
			RouteFound:  true,
			RouteTarget: compose.RouteTarget{ServiceName: "web", Port: 3100},
		}},
		Routing: testRouting(t, sqlite, routeSyncer, nil),
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorCompose(t, gotWorktreePath, `
services:
  web:
    image: example/web:latest
    ports:
      - "127.0.0.1:3100:80"
`)
			return nil
		}),
		Now: func() time.Time { return now },
	})

	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	domains, err := sqlite.ListDomainsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("domains = %#v, want one initial route", domains)
	}
	if len(routeSyncer.Syncs) != 1 {
		t.Fatalf("router syncs = %#v, want one sync", routeSyncer.Syncs)
	}
	wantRoutes := []router.Route{{AppID: "my-app", ServiceName: "web", DomainName: "my-app.example.com", Port: 3100, HTTPS: true}}
	if !reflect.DeepEqual(routeSyncer.Syncs[0], wantRoutes) {
		t.Fatalf("router sync = %#v, want %#v", routeSyncer.Syncs[0], wantRoutes)
	}

	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.queued", "deploy.started", "deploy.succeeded", "route.auto_attached", "router.reloaded"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
}

func TestExecutorDoesNotAutoRouteFailedDeploy(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "sshdock.example.com",
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	routeSyncer := &fakeExecutorRouteSyncer{}
	clock := newExecutorClock(now)
	handler := newTestExecutor(t, Config{
		Store:   sqlite,
		Runner:  &compose.FakeRunner{DeployErr: errors.New("compose failed")},
		Routing: testRouting(t, sqlite, routeSyncer, nil),
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorCompose(t, gotWorktreePath, `
services:
  web:
    image: example/web:latest
    ports:
      - "3100:80"
`)
			return nil
		}),
		Now: clock.Now,
	})

	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123"); err == nil {
		t.Fatal("Execute error = nil, want deploy failure")
	}
	domains, err := sqlite.ListDomainsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("domains = %#v, want none", domains)
	}
	if len(routeSyncer.Syncs) != 0 {
		t.Fatalf("router syncs = %#v, want none", routeSyncer.Syncs)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.queued", "deploy.started", "deploy.failed"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
}

func TestExecutorPassesResolvedConfigEnvironment(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	runner := &compose.FakeRunner{}
	resolver := &fakeExecutorConfigResolver{env: map[string]string{"DATABASE_URL": "postgres://secret", "ROOT_COMPOSE": "compose.yml"}}
	handler := newTestExecutor(t, Config{
		Store:          sqlite,
		Runner:         runner,
		ConfigResolver: resolver,
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorCompose(t, gotWorktreePath, `
services:
  base:
    image: example/base:latest
  web:
    extends:
      file: ${ROOT_COMPOSE}
      service: base
`)
			return nil
		}),
	})

	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(resolver.requests) != 1 || resolver.requests[0] != (executorConfigResolveRequest{appID: "my-app"}) {
		t.Fatalf("resolver requests = %#v", resolver.requests)
	}
	if len(runner.DeployRequests) != 1 || runner.DeployRequests[0].Env["DATABASE_URL"] != "postgres://secret" {
		t.Fatalf("deploy requests = %#v", runner.DeployRequests)
	}
}

func TestExecutorRecordsValidationFailureBeforeFakeDeploy(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	runner := &compose.FakeRunner{}
	handler := newTestExecutor(t, Config{
		Store:          sqlite,
		Runner:         runner,
		ConfigResolver: &fakeExecutorConfigResolver{env: map[string]string{"ROOT_COMPOSE": "shared.compose.yml"}},
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorCompose(t, gotWorktreePath, `
services:
  web:
    extends:
      file: ${ROOT_COMPOSE}
      service: base
`)
			return nil
		}),
	})

	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123")
	if err == nil || !strings.Contains(err.Error(), "external Compose file") {
		t.Fatalf("Execute error = %v, want external Compose validation failure", err)
	}
	if strings.Contains(err.Error(), "shared.compose.yml") {
		t.Fatalf("Execute error disclosed resolved config value: %v", err)
	}
	if len(runner.DeployRequests) != 0 {
		t.Fatalf("deploy requests = %#v, want none", runner.DeployRequests)
	}
	if status := queryDeploymentStatus(t, dbPath, "dep_abc123"); status != string(app.DeploymentStatusFailed) {
		t.Fatalf("deployment status = %q, want failed", status)
	}
	events, listErr := sqlite.ListEventsByApp(ctx, "my-app")
	if listErr != nil {
		t.Fatalf("ListEventsByApp: %v", listErr)
	}
	if got, want := eventTypes(events), []string{"deploy.queued", "deploy.started", "deploy.failed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %#v, want %#v", got, want)
	}
}

func TestExecutorConfigFailureStopsBeforeCompose(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	runner := &compose.FakeRunner{}
	missingConfig := errors.New("missing required config for my-app: SECRET\nssh sshdock@<host> config set my-app SECRET")
	resolver := &fakeExecutorConfigResolver{err: missingConfig}
	handler := newTestExecutor(t, Config{
		Store:          sqlite,
		Runner:         runner,
		ConfigResolver: resolver,
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_config_attempt")
	if !errors.Is(err, missingConfig) {
		t.Fatalf("Execute error = %v, want wrapped %v", err, missingConfig)
	}
	assertFailureDetail(t, err.Error(),
		"stage=config",
		"detail=missing required config for my-app: SECRET",
		"ssh sshdock@<host> config set my-app SECRET",
		"changed=release rel_my-app_abc123 and deployment dep_config_attempt marked failed before Compose started; containers and routes were not changed",
		"fix=set the missing config value(s) with the command(s) in detail",
		"retry=sudo sshdock apps redeploy my-app",
	)
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("Execute error leaked config value: %v", err)
	}
	if len(runner.DeployRequests) != 0 {
		t.Fatalf("deploy requests = %#v, want none", runner.DeployRequests)
	}
	if status := queryDeploymentStatus(t, dbPath, "dep_config_attempt"); status != string(app.DeploymentStatusFailed) {
		t.Fatalf("deployment status = %q", status)
	}
	errorMessage := queryDeploymentError(t, dbPath, "dep_config_attempt")
	assertFailureDetail(t, errorMessage, "stage=config", "changed=release rel_my-app_abc123", "retry=sudo sshdock apps redeploy my-app")
	deployments, deploymentErr := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if deploymentErr != nil {
		t.Fatalf("ListDeploymentsByApp: %v", deploymentErr)
	}
	if len(deployments) != 1 || deployments[0].FailureStage != "config" || deployments[0].FailureDetail == "" || deployments[0].RetryGuidance != "sudo sshdock apps redeploy my-app" {
		t.Fatalf("deployment metadata = %#v", deployments)
	}
	events, eventErr := sqlite.ListEventsByApp(ctx, "my-app")
	if eventErr != nil {
		t.Fatalf("ListEventsByApp: %v", eventErr)
	}
	if !strings.Contains(events[2].Message, errorMessage) {
		t.Fatalf("failed event = %#v, want deployment failure detail %q", events[2], errorMessage)
	}
}

func TestExecutorRecordsAutoRouteSkippedForUnsafeInference(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "sshdock.example.com",
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	routeSyncer := &fakeExecutorRouteSyncer{}
	handler := newTestExecutor(t, Config{
		Store:   sqlite,
		Runner:  &compose.FakeRunner{DeployResult: compose.DeployResult{RouteReason: "effective Compose model route is ambiguous"}},
		Routing: testRouting(t, sqlite, routeSyncer, nil),
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorCompose(t, gotWorktreePath, `
services:
  api:
    image: example/api:latest
    ports:
      - "4100:80"
  admin:
    image: example/admin:latest
    ports:
      - "4200:80"
`)
			return nil
		}),
		Now: func() time.Time { return now },
	})

	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	domains, err := sqlite.ListDomainsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("domains = %#v, want none", domains)
	}
	if len(routeSyncer.Syncs) != 0 {
		t.Fatalf("router syncs = %#v, want none", routeSyncer.Syncs)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.queued", "deploy.started", "deploy.succeeded", "route.auto_skipped"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
	assertFailureDetail(t, events[3].Message, "stage=route inference", "ambiguous", "changed=containers deployed, routes unchanged", "domains attach")
}

func TestExecutorPrintsNoRouteGuidanceWithoutBaseDomain(t *testing.T) {
	// Given
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	var output strings.Builder
	handler := newTestExecutor(t, Config{
		Store:  sqlite,
		Runner: &compose.FakeRunner{DeployResult: compose.DeployResult{RouteReason: "effective Compose model has no route candidate"}},
		Output: &output,
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	// When
	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123")

	// Then
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output.String(), "sudo sshdock domains attach my-app <service> <domain> --port <port>") {
		t.Fatalf("output = %q, want manual route guidance", output.String())
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.queued", "deploy.started", "deploy.succeeded", "route.auto_skipped"}
	if got := eventTypes(events); !reflect.DeepEqual(got, wantTypes) {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
}

func TestExecutorRecordsCaddyReloadFailureWithRecoveryGuidance(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "sshdock.example.com",
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	routeSyncer := &fakeExecutorRouteSyncer{Err: errors.New("caddy reload failed")}
	handler := newTestExecutor(t, Config{
		Store: sqlite,
		Runner: &compose.FakeRunner{DeployResult: compose.DeployResult{
			RouteFound:  true,
			RouteTarget: compose.RouteTarget{ServiceName: "web", Port: 3100},
		}},
		Routing: testRouting(t, sqlite, routeSyncer, nil),
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorCompose(t, gotWorktreePath, `
services:
  web:
    image: example/web:latest
    ports:
      - "127.0.0.1:3100:80"
`)
			return nil
		}),
		Now: func() time.Time { return now },
	})

	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.queued", "deploy.started", "deploy.succeeded", "route.auto_attached", "router.reload_failed"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
	assertFailureDetail(t, events[4].Message,
		"stage=caddy reload",
		"detail=caddy reload failed",
		"changed=domain was stored, but generated Caddy routes may not be active",
		"fix=run sudo sshdock diagnostics",
		"retry=sudo sshdock domains attach my-app web my-app.example.com --port 3100",
	)
}

func TestExecutorRecordsAutoRouteSkippedForDNSUnsafeAppName(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStoreForApp(t, ctx, dbPath, "bad_app")
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "sshdock.example.com",
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	handler := newTestExecutor(t, Config{
		Store: sqlite,
		Runner: &compose.FakeRunner{DeployResult: compose.DeployResult{
			RouteFound:  true,
			RouteTarget: compose.RouteTarget{ServiceName: "web", Port: 3100},
		}},
		Routing: testRouting(t, sqlite, &fakeExecutorRouteSyncer{}, nil),
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorCompose(t, gotWorktreePath, `
services:
  web:
    image: example/web:latest
    ports:
      - "3100:80"
`)
			return nil
		}),
		Now: func() time.Time { return now },
	})

	if err := executeAttempt(t, ctx, sqlite, handler, "bad_app", "abc123", "dep_abc123"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	domains, err := sqlite.ListDomainsByApp(ctx, "bad_app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("domains = %#v, want none", domains)
	}
	events, err := sqlite.ListEventsByApp(ctx, "bad_app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.queued", "deploy.started", "deploy.succeeded", "route.auto_skipped"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
	if !strings.Contains(events[3].Message, "DNS label") {
		t.Fatalf("skip event = %#v, want DNS label guidance", events[3])
	}
}

func TestExecutorPrintsAndRecordsTrustedComposeWarnings(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	warning := "service web uses privileged mode; trusted Compose pushes have host-level impact; SSHDock does not sandbox this configuration"
	runner := &compose.FakeRunner{DeployResult: compose.DeployResult{
		RouteFound:  true,
		RouteTarget: compose.RouteTarget{ServiceName: "web", Port: 3000},
		Warnings:    []string{warning},
	}}
	var output strings.Builder
	handler := newTestExecutor(t, Config{
		Store:  sqlite,
		Runner: runner,
		Output: &output,
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output.String(), "warning: "+warning) {
		t.Fatalf("output = %q, want trusted Compose warning", output.String())
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	wantTypes := []string{"deploy.queued", "deploy.started", "deploy.warning", "deploy.succeeded"}
	if got := eventTypes(events); !reflect.DeepEqual(got, wantTypes) {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
	if events[2].Message != warning {
		t.Fatalf("warning event = %#v, want %q", events[2], warning)
	}
}

func TestExecutorRedactsConfigValuesFromTrustedComposeWarnings(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	secretPath := "/srv/private/customer-secret"
	warning := "service web uses host bind mount " + secretPath + "; trusted Compose pushes have host-level impact; SSHDock does not sandbox this configuration"
	var output strings.Builder
	handler := newTestExecutor(t, Config{
		Store:          sqlite,
		Runner:         &compose.FakeRunner{DeployResult: compose.DeployResult{Warnings: []string{warning}}},
		ConfigResolver: &fakeExecutorConfigResolver{env: map[string]string{"PRIVATE_PATH": secretPath}},
		Output:         &output,
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(output.String(), secretPath) || !strings.Contains(output.String(), "<redacted>") {
		t.Fatalf("output = %q, want redacted warning", output.String())
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if strings.Contains(events[2].Message, secretPath) || !strings.Contains(events[2].Message, "<redacted>") {
		t.Fatalf("warning event = %#v, want redacted value", events[2])
	}
}

func TestExecutorFinishesSuccessfulDeployWhenWarningOutputFails(t *testing.T) {
	// Given
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	handler := newTestExecutor(t, Config{
		Store: sqlite,
		Runner: &compose.FakeRunner{DeployResult: compose.DeployResult{
			RouteFound:  true,
			RouteTarget: compose.RouteTarget{ServiceName: "web", Port: 3000},
			Warnings:    []string{"trusted Compose warning"},
		}},
		Output: failingWriter{},
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	// When
	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123")

	// Then
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(events[len(events)-1].Message, "output unavailable") {
		t.Fatalf("success event = %#v, want warning output failure recorded", events[len(events)-1])
	}
	if status := queryDeploymentStatus(t, dbPath, "dep_abc123"); status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q, want succeeded", status)
	}
}

func TestExecutorMarksDeploymentFailedWhenDeployFails(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	deployFailure := errors.New("pull access denied")
	failure := compose.NewDeployError(compose.DeployStagePullImages, deployFailure)
	runner := &compose.FakeRunner{DeployErr: failure}
	handler := newTestExecutor(t, Config{
		Store:  sqlite,
		Runner: runner,
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123")
	if !errors.Is(err, deployFailure) {
		t.Fatalf("Execute error = %v, want wrapped %v", err, deployFailure)
	}
	if len(runner.DeployRequests) != 1 {
		t.Fatalf("DeployRequests = %#v, want one attempt and no automatic rollback", runner.DeployRequests)
	}

	status := queryDeploymentStatus(t, dbPath, "dep_abc123")
	if status != string(app.DeploymentStatusFailed) {
		t.Fatalf("deployment status = %q", status)
	}
	errorMessage := queryDeploymentError(t, dbPath, "dep_abc123")
	assertFailureDetail(t, errorMessage,
		"stage=pull images",
		"detail=pull images failed: pull access denied",
		"changed=release rel_my-app_abc123 and deployment dep_abc123 marked failed before containers started; routes were not changed",
		"fix=check image names, registry credentials, and network access",
		"retry=push a fixed commit to remote main, or run sudo sshdock apps redeploy my-app after a transient fix",
	)

	model, getErr := sqlite.GetApp(ctx, "my-app")
	if getErr != nil {
		t.Fatalf("GetApp: %v", getErr)
	}
	if model.Status != app.AppStatusFailed {
		t.Fatalf("app status = %q, want %q", model.Status, app.AppStatusFailed)
	}

	releases, listErr := sqlite.ListReleasesByApp(ctx, "my-app")
	if listErr != nil {
		t.Fatalf("ListReleasesByApp: %v", listErr)
	}
	if len(releases) != 1 || releases[0].Status != app.ReleaseStatusFailed {
		t.Fatalf("releases = %#v, want one failed release", releases)
	}

	events, listEventsErr := sqlite.ListEventsByApp(ctx, "my-app")
	if listEventsErr != nil {
		t.Fatalf("ListEventsByApp: %v", listEventsErr)
	}
	wantTypes := []string{"deploy.queued", "deploy.started", "deploy.failed"}
	if got := eventTypes(events); strings.Join(got, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("event types = %#v, want %#v", got, wantTypes)
	}
}

func TestExecutorRedactsConfigValuesFromDeployFailure(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	failure := errors.New("docker output included api-secret")
	handler := newTestExecutor(t, Config{
		Store:          sqlite,
		Runner:         &compose.FakeRunner{DeployErr: failure},
		ConfigResolver: &fakeExecutorConfigResolver{env: map[string]string{"API_TOKEN": "api-secret"}},
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123")
	if !errors.Is(err, failure) {
		t.Fatalf("Execute error = %v, want wrapped failure", err)
	}
	if strings.Contains(err.Error(), "api-secret") {
		t.Fatalf("Execute error leaked secret: %v", err)
	}
	errorMessage := queryDeploymentError(t, dbPath, "dep_abc123")
	assertFailureDetail(t, errorMessage,
		"stage=deploy",
		"detail=docker output included <redacted>",
		"changed=release rel_my-app_abc123 and deployment dep_abc123 marked failed; inspect the detail before assuming container or route state",
		"retry=push a fixed commit to remote main, or run sudo sshdock apps redeploy my-app after a transient fix",
	)
	if strings.Contains(errorMessage, "api-secret") {
		t.Fatalf("deployment error leaked secret: %q", errorMessage)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if strings.Contains(events[2].Message, "api-secret") {
		t.Fatalf("event leaked secret: %#v", events[2])
	}
}

func TestExecutorKeepsExistingRouteAfterSuccessfulReplacement(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{BaseDomain: "example.com", GitHost: "sshdock.example.com"}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	createdAt := time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC)
	if err := sqlite.AttachDomain(ctx, app.Domain{
		ID:          "dom_existing",
		AppID:       "my-app",
		ServiceName: "web",
		DomainName:  "my-app.example.com",
		Port:        3100,
		HTTPS:       true,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	routeSyncer := &fakeExecutorRouteSyncer{}
	handler := newTestExecutor(t, Config{
		Store: sqlite,
		Runner: &compose.FakeRunner{DeployResult: compose.DeployResult{
			RouteFound:  true,
			RouteTarget: compose.RouteTarget{ServiceName: "replacement", Port: 4100},
		}},
		Routing: testRouting(t, sqlite, routeSyncer, nil),
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	domains, err := sqlite.ListDomainsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 1 || domains[0].ServiceName != "web" || domains[0].Port != 3100 {
		t.Fatalf("domains = %#v, want unchanged web:3100 route", domains)
	}
	if len(routeSyncer.Syncs) != 0 {
		t.Fatalf("router syncs = %#v, want none for existing route", routeSyncer.Syncs)
	}
}

func TestExecutorDoesNotSuggestInitialRouteForAlreadyRoutedApp(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sshdock.db")
	sqlite := newExecutorTestStore(t, ctx, dbPath)
	createdAt := time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC)
	if err := sqlite.AttachDomain(ctx, app.Domain{
		ID:          "dom_existing",
		AppID:       "my-app",
		ServiceName: "web",
		DomainName:  "my-app.example.com",
		Port:        3100,
		HTTPS:       true,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	var output strings.Builder
	handler := newTestExecutor(t, Config{
		Store:  sqlite,
		Runner: &compose.FakeRunner{DeployResult: compose.DeployResult{RouteReason: "effective Compose model has no route candidate"}},
		Output: &output,
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_abc123"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(output.String(), "stage=route inference") {
		t.Fatalf("output = %q, want no initial-route guidance for an already-routed app", output.String())
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if got, want := eventTypes(events), []string{"deploy.queued", "deploy.started", "deploy.succeeded"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %#v, want %#v", got, want)
	}
}

func newExecutorTestStore(t *testing.T, ctx context.Context, dbPath string) *store.SQLiteStore {
	return newExecutorTestStoreForApp(t, ctx, dbPath, "my-app")
}

func newExecutorTestStoreForApp(t *testing.T, ctx context.Context, dbPath string, appName string) *store.SQLiteStore {
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

func writeExecutorComposeFixture(t *testing.T, worktreePath string) {
	t.Helper()

	writeExecutorCompose(t, worktreePath, `
services:
  web:
    image: example/web:latest
`)
}

func writeExecutorCompose(t *testing.T, worktreePath string, content string) {
	t.Helper()

	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

type fakeExecutorRouteSyncer struct {
	Syncs [][]router.Route
	Err   error
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	return 0, errors.New("output unavailable")
}

type executorConfigResolveRequest struct {
	appID string
}

type fakeExecutorConfigResolver struct {
	env      map[string]string
	err      error
	requests []executorConfigResolveRequest
}

func (f *fakeExecutorConfigResolver) ResolveEnv(_ context.Context, appID string) (map[string]string, error) {
	f.requests = append(f.requests, executorConfigResolveRequest{appID: appID})
	return f.env, f.err
}

func (f *fakeExecutorRouteSyncer) SyncRoutes(_ context.Context, routes []router.Route) error {
	copied := append([]router.Route(nil), routes...)
	f.Syncs = append(f.Syncs, copied)
	return f.Err
}

type executorClock struct {
	next time.Time
}

func newExecutorClock(start time.Time) *executorClock {
	return &executorClock{next: start}
}

func (c *executorClock) Now() time.Time {
	value := c.next
	c.next = c.next.Add(time.Second)
	return value
}

func queryDeploymentStatus(t *testing.T, dbPath string, deploymentID string) string {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	var status string
	if err := db.QueryRow(`select status from deployments where id = ?`, deploymentID).Scan(&status); err != nil {
		t.Fatalf("query deployment status: %v", err)
	}
	return status
}

func queryDeploymentError(t *testing.T, dbPath string, deploymentID string) string {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	var errorMessage string
	if err := db.QueryRow(`select error_message from deployments where id = ?`, deploymentID).Scan(&errorMessage); err != nil {
		t.Fatalf("query deployment error: %v", err)
	}
	return errorMessage
}

func assertFailureDetail(t *testing.T, message string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(message, want) {
			t.Fatalf("failure detail missing %q:\n%s", want, message)
		}
	}
}

func eventTypes(events []app.Event) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

type executorCheckoutFunc func(context.Context, string, string, string) error

func (f executorCheckoutFunc) Checkout(ctx context.Context, repoPath, worktreePath, commitSHA string) error {
	return f(ctx, repoPath, worktreePath, commitSHA)
}

func executeAttempt(t *testing.T, ctx context.Context, sqlite *store.SQLiteStore, executor *Executor, appID, commitSHA, deploymentID string) error {
	t.Helper()
	attempt := app.Deployment{ID: deploymentID, AppID: appID, ReleaseID: app.ReleaseID(appID, commitSHA), CommitSHA: commitSHA, Trigger: app.DeploymentTriggerRedeploy, Status: app.DeploymentStatusPending, StartedAt: executor.now()}
	if err := sqlite.QueueRedeployment(ctx, attempt); err != nil {
		t.Fatalf("QueueRedeployment: %v", err)
	}
	claimed, found, err := sqlite.ClaimNextPendingDeployment(ctx)
	if err != nil || !found || claimed.ID != attempt.ID {
		t.Fatalf("ClaimNextPendingDeployment = %#v, %v, %v; want %s", claimed, found, err, attempt.ID)
	}
	return executor.Execute(ctx, claimed)
}

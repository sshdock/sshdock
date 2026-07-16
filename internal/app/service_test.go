package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestServiceCreatesAndListsApps(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(store, WithClock(func() time.Time { return now }))

	created, err := service.CreateApp(ctx, App{
		ID:     "app_1",
		Name:   "my-app",
		NodeID: "local",
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	if created.Status != AppStatusCreated {
		t.Fatalf("Status = %q, want %q", created.Status, AppStatusCreated)
	}
	if !created.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %s, want %s", created.CreatedAt, now)
	}
	if !created.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %s, want %s", created.UpdatedAt, now)
	}

	apps, err := service.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0] != created {
		t.Fatalf("ListApps = %#v, want [%#v]", apps, created)
	}
}

func TestServiceCreatesRelease(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(store, WithClock(func() time.Time { return now }))

	release, err := service.CreateRelease(ctx, Release{
		ID:          "rel_1",
		AppID:       "app_1",
		CommitSHA:   "abc123",
		ComposePath: "compose.yml",
	})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	if release.Status != ReleaseStatusPending {
		t.Fatalf("Status = %q, want %q", release.Status, ReleaseStatusPending)
	}
	if !release.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %s, want %s", release.CreatedAt, now)
	}
	if !release.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %s, want %s", release.UpdatedAt, now)
	}
}

func TestServiceDeploymentLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	startedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)
	service := NewService(store, WithClock(func() time.Time { return startedAt }))

	deployment, err := service.StartDeployment(ctx, Deployment{
		ID:        "dep_1",
		AppID:     "app_1",
		ReleaseID: "rel_1",
	})
	if err != nil {
		t.Fatalf("StartDeployment: %v", err)
	}

	if deployment.Status != DeploymentStatusDeploying {
		t.Fatalf("Status = %q, want %q", deployment.Status, DeploymentStatusDeploying)
	}
	if !deployment.StartedAt.Equal(startedAt) {
		t.Fatalf("StartedAt = %s, want %s", deployment.StartedAt, startedAt)
	}

	service = NewService(store, WithClock(func() time.Time { return finishedAt }))
	if err := service.MarkDeploymentSucceeded(ctx, deployment.ID); err != nil {
		t.Fatalf("MarkDeploymentSucceeded: %v", err)
	}
	if store.deploymentStatuses[deployment.ID] != DeploymentStatusSucceeded {
		t.Fatalf("deployment status = %q", store.deploymentStatuses[deployment.ID])
	}
	if !store.deploymentFinishedAt[deployment.ID].Equal(finishedAt) {
		t.Fatalf("finished at = %s", store.deploymentFinishedAt[deployment.ID])
	}

	if err := service.MarkDeploymentFailed(ctx, deployment.ID, "compose failed"); err != nil {
		t.Fatalf("MarkDeploymentFailed: %v", err)
	}
	if store.deploymentStatuses[deployment.ID] != DeploymentStatusFailed {
		t.Fatalf("deployment status = %q", store.deploymentStatuses[deployment.ID])
	}
	if store.deploymentErrors[deployment.ID] != "compose failed" {
		t.Fatalf("deployment error = %q", store.deploymentErrors[deployment.ID])
	}
}

func TestServiceAttachesDomain(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(store, WithClock(func() time.Time { return now }))

	domain, err := service.AttachDomain(ctx, Domain{
		ID:          "dom_1",
		AppID:       "app_1",
		ServiceName: "web",
		DomainName:  "example.com",
		Port:        3000,
		HTTPS:       true,
	})
	if err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}

	if !domain.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %s, want %s", domain.CreatedAt, now)
	}
	if !domain.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %s, want %s", domain.UpdatedAt, now)
	}
	if len(store.domains[domain.AppID]) != 1 || store.domains[domain.AppID][0] != domain {
		t.Fatalf("stored domains = %#v", store.domains[domain.AppID])
	}
}

func TestServiceRestartAppAndServiceUseCurrentWorktreeComposeEntry(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	worktreePath := t.TempDir()
	composePath := filepath.Join(worktreePath, "compose.yml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write current Compose file: %v", err)
	}
	runner := &fakeRecoveryRunner{}
	service := NewService(store, WithClock(func() time.Time { return now }), WithRecoveryRunner(runner))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: worktreePath, Status: AppStatusHealthy}
	store.releases["rel_bad"] = Release{ID: "rel_bad", AppID: "app_1", CommitSHA: "bad", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusFailed, CreatedAt: now.Add(-time.Hour)}
	store.releases["rel_good"] = Release{ID: "rel_good", AppID: "app_1", CommitSHA: "good", ComposePath: "/historical/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	// When
	if err := service.RestartApp(ctx, "app_1"); err != nil {
		t.Fatalf("RestartApp: %v", err)
	}
	if err := service.RestartService(ctx, "app_1", "web"); err != nil {
		t.Fatalf("RestartService: %v", err)
	}

	if len(runner.restarts) != 2 {
		t.Fatalf("restart requests = %#v", runner.restarts)
	}
	appRestart := runner.restarts[0]
	if appRestart.AppName != "app_1" || appRestart.ProjectDir != worktreePath || appRestart.ComposePath != composePath || appRestart.ServiceName != "" {
		t.Fatalf("app restart request = %#v", appRestart)
	}
	serviceRestart := runner.restarts[1]
	if serviceRestart.ServiceName != "web" {
		t.Fatalf("service restart request = %#v", serviceRestart)
	}
	assertEventTypes(t, store.events["app_1"], []string{"restart.started", "restart.succeeded", "service.restart.started", "service.restart.succeeded"})
}

func TestServiceRedeployCurrentMainUsesFailedCurrentCommitInsteadOfLatestGood(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	worktreePath := testComposeWorktree(t)
	runner := &fakeRecoveryRunner{}
	service := NewService(
		store,
		WithClock(func() time.Time { return now }),
		WithDeployRunner(runner),
		WithCurrentMainResolver(CurrentMainResolverFunc(func(_ context.Context, repoPath string) (string, error) {
			if repoPath != "/apps/app_1/repo.git" {
				t.Fatalf("repo path = %q", repoPath)
			}
			return "new", nil
		})),
	)
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", RepoPath: "/apps/app_1/repo.git", WorktreePath: worktreePath, Status: AppStatusFailed}
	store.releases["rel_old"] = Release{ID: "rel_old", AppID: "app_1", CommitSHA: "old", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}
	store.releases["rel_new"] = Release{ID: "rel_new", AppID: "app_1", CommitSHA: "new", ComposePath: "/historical/rolled-back/compose.yml", Status: ReleaseStatusRolledBack, CreatedAt: now.Add(-time.Hour)}

	// When
	deployment, err := service.RedeployCurrentMain(ctx, "app_1", "dep_redeploy_1")

	// Then
	if err != nil {
		t.Fatalf("RedeployCurrentMain: %v", err)
	}
	if deployment.ReleaseID != "rel_new" || deployment.CommitSHA != "new" {
		t.Fatalf("deployment = %#v, want current main release", deployment)
	}
	if len(runner.deploys) != 1 || runner.deploys[0].CommitSHA != "new" || runner.deploys[0].ComposePath != filepath.Join(worktreePath, "compose.yml") {
		t.Fatalf("deploy requests = %#v, want current main commit", runner.deploys)
	}
	for index, want := range []string{"Redeploy current main new started", "Redeploy current main new succeeded"} {
		if got := store.events["app_1"][index].Message; got != want {
			t.Fatalf("event[%d] message = %q, want %q", index, got, want)
		}
	}
}

func TestServiceRedeployCurrentMainCreatesDeploymentAndUpdatesState(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	worktreePath := testComposeWorktree(t)
	runner := &fakeRecoveryRunner{}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: worktreePath, Status: AppStatusHealthy}
	store.releases["rel_old"] = Release{ID: "rel_old", AppID: "app_1", CommitSHA: "old", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now.Add(-time.Hour)}
	store.releases["rel_new"] = Release{ID: "rel_new", AppID: "app_1", CommitSHA: "new", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	deployment, err := service.RedeployCurrentMain(ctx, "app_1", "dep_redeploy_1")
	if err != nil {
		t.Fatalf("RedeployCurrentMain: %v", err)
	}
	if deployment.ReleaseID != "rel_new" {
		t.Fatalf("deployment release = %q", deployment.ReleaseID)
	}
	if deployment.CommitSHA != "new" || deployment.Trigger != DeploymentTriggerRedeploy {
		t.Fatalf("deployment identity metadata = %#v", deployment)
	}
	if len(runner.deploys) != 1 {
		t.Fatalf("deploy requests = %#v", runner.deploys)
	}
	request := runner.deploys[0]
	if request.ReleaseID != "rel_new" || request.CommitSHA != "new" || request.ProjectDir != worktreePath || request.ComposePath != filepath.Join(worktreePath, "compose.yml") {
		t.Fatalf("deploy request = %#v", request)
	}
	if store.appStatuses["app_1"] != AppStatusHealthy {
		t.Fatalf("app status = %q", store.appStatuses["app_1"])
	}
	if store.releaseStatuses["rel_new"] != ReleaseStatusSucceeded {
		t.Fatalf("release status = %q", store.releaseStatuses["rel_new"])
	}
	if store.deploymentStatuses["dep_redeploy_1"] != DeploymentStatusSucceeded {
		t.Fatalf("deployment status = %q", store.deploymentStatuses["dep_redeploy_1"])
	}
	assertEventTypes(t, store.events["app_1"], []string{"redeploy.started", "redeploy.succeeded"})
}

func TestServiceRedeployCurrentMainPassesResolvedConfigEnvironment(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	worktreePath := testComposeWorktree(t)
	runner := &fakeRecoveryRunner{}
	resolver := &fakeConfigResolver{env: map[string]string{"DATABASE_URL": "postgres://secret"}}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithConfigResolver(resolver), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: worktreePath, Status: AppStatusHealthy}
	store.releases["rel_new"] = Release{ID: "rel_new", AppID: "app_1", CommitSHA: "new", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	if _, err := service.RedeployCurrentMain(ctx, "app_1", "dep_redeploy_1"); err != nil {
		t.Fatalf("RedeployCurrentMain: %v", err)
	}
	if len(resolver.requests) != 1 {
		t.Fatalf("resolver requests = %#v", resolver.requests)
	}
	if resolver.requests[0] != (configResolveRequest{appID: "app_1"}) {
		t.Fatalf("resolver request = %#v", resolver.requests[0])
	}
	if len(runner.deploys) != 1 || runner.deploys[0].Env["DATABASE_URL"] != "postgres://secret" {
		t.Fatalf("deploy requests = %#v", runner.deploys)
	}
}

func TestServiceRedeployConfigFailureStopsBeforeCompose(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	worktreePath := testComposeWorktree(t)
	runner := &fakeRecoveryRunner{}
	resolver := &fakeConfigResolver{err: errors.New("missing required config for app_1: SECRET")}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithConfigResolver(resolver), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: worktreePath, Status: AppStatusHealthy}
	store.releases["rel_new"] = Release{ID: "rel_new", AppID: "app_1", CommitSHA: "new", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	_, err := service.RedeployCurrentMain(ctx, "app_1", "dep_redeploy_1")
	if err == nil || err.Error() != "missing required config for app_1: SECRET" {
		t.Fatalf("RedeployCurrentMain error = %v", err)
	}
	if len(runner.deploys) != 0 {
		t.Fatalf("compose deploys = %#v, want none", runner.deploys)
	}
	if store.deploymentStatuses["dep_redeploy_1"] != DeploymentStatusFailed {
		t.Fatalf("deployment status = %q", store.deploymentStatuses["dep_redeploy_1"])
	}
	storedAttempt := store.deployments["dep_redeploy_1"]
	if storedAttempt.FailureStage != "config" || storedAttempt.FailureDetail == "" || storedAttempt.RetryGuidance != "sudo sshdock apps redeploy app_1" {
		t.Fatalf("deployment failure metadata = %#v", storedAttempt)
	}
	if store.deploymentErrors["dep_redeploy_1"] != "missing required config for app_1: SECRET" {
		t.Fatalf("deployment error = %q", store.deploymentErrors["dep_redeploy_1"])
	}
}

func TestServiceRedeployFailureMarksAppAndDeploymentFailed(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	worktreePath := testComposeWorktree(t)
	deployFailure := errors.New("compose failed")
	failure := compose.NewDeployError(compose.DeployStageBuildServices, deployFailure)
	runner := &fakeRecoveryRunner{deployErr: failure}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: worktreePath, Status: AppStatusHealthy}
	store.releases["rel_new"] = Release{ID: "rel_new", AppID: "app_1", CommitSHA: "new", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	_, err := service.RedeployCurrentMain(ctx, "app_1", "dep_redeploy_1")
	if !errors.Is(err, deployFailure) {
		t.Fatalf("RedeployCurrentMain error = %v, want wrapped %v", err, deployFailure)
	}
	if store.appStatuses["app_1"] != AppStatusFailed {
		t.Fatalf("app status = %q", store.appStatuses["app_1"])
	}
	if store.deploymentStatuses["dep_redeploy_1"] != DeploymentStatusFailed {
		t.Fatalf("deployment status = %q", store.deploymentStatuses["dep_redeploy_1"])
	}
	for _, want := range []string{
		"stage=build services",
		"detail=build services failed: compose failed",
		"changed=redeploy deployment dep_redeploy_1 marked failed; the previously running release may still be serving",
		"fix=fix Dockerfile or build context errors",
		"retry=sudo sshdock apps redeploy app_1",
	} {
		if !strings.Contains(store.deploymentErrors["dep_redeploy_1"], want) {
			t.Fatalf("deployment error missing %q:\n%s", want, store.deploymentErrors["dep_redeploy_1"])
		}
	}
	assertEventTypes(t, store.events["app_1"], []string{"redeploy.started", "redeploy.failed"})
}

func TestServiceRedeployFailureRedactsResolvedConfigValues(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	worktreePath := testComposeWorktree(t)
	failure := errors.New("docker output included postgres://secret")
	runner := &fakeRecoveryRunner{deployErr: failure}
	resolver := &fakeConfigResolver{env: map[string]string{"DATABASE_URL": "postgres://secret"}}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithConfigResolver(resolver), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: worktreePath, Status: AppStatusHealthy}
	store.releases["rel_new"] = Release{ID: "rel_new", AppID: "app_1", CommitSHA: "new", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	_, err := service.RedeployCurrentMain(ctx, "app_1", "dep_redeploy_1")
	if !errors.Is(err, failure) {
		t.Fatalf("RedeployCurrentMain error = %v, want wrapped failure", err)
	}
	if strings.Contains(err.Error(), "postgres://secret") {
		t.Fatalf("returned error leaked secret: %v", err)
	}
	if !strings.Contains(store.deploymentErrors["dep_redeploy_1"], "detail=docker output included <redacted>") {
		t.Fatalf("deployment error = %q", store.deploymentErrors["dep_redeploy_1"])
	}
	if strings.Contains(store.deploymentErrors["dep_redeploy_1"], "postgres://secret") {
		t.Fatalf("deployment error leaked secret: %q", store.deploymentErrors["dep_redeploy_1"])
	}
	if strings.Contains(store.events["app_1"][1].Message, "postgres://secret") {
		t.Fatalf("event leaked secret: %#v", store.events["app_1"][1])
	}
}

type fakeServiceStore struct {
	apps                 map[string]App
	releases             map[string]Release
	deployments          map[string]Deployment
	appStatuses          map[string]AppStatus
	releaseStatuses      map[string]ReleaseStatus
	deploymentStatuses   map[string]DeploymentStatus
	deploymentFinishedAt map[string]time.Time
	deploymentErrors     map[string]string
	domains              map[string][]Domain
	events               map[string][]Event
}

func withCurrentMain(commitSHA string) ServiceOption {
	return WithCurrentMainResolver(CurrentMainResolverFunc(func(context.Context, string) (string, error) {
		return commitSHA, nil
	}))
}

func testComposeWorktree(t *testing.T) string {
	t.Helper()
	worktreePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile Compose: %v", err)
	}
	return worktreePath
}

type fakeRecoveryRunner struct {
	deploys  []compose.DeployRequest
	starts   []compose.LifecycleRequest
	stops    []compose.LifecycleRequest
	restarts []compose.RestartRequest

	deployErr  error
	startErr   error
	stopErr    error
	restartErr error
}

type configResolveRequest struct {
	appID string
}

type fakeConfigResolver struct {
	env      map[string]string
	err      error
	requests []configResolveRequest
}

func (f *fakeConfigResolver) ResolveAppConfig(ctx context.Context, appID string) (map[string]string, error) {
	f.requests = append(f.requests, configResolveRequest{appID: appID})
	return f.env, f.err
}

type checkoutCall struct {
	repoPath     string
	worktreePath string
	commitSHA    string
}

type fakeWorktreeCheckout struct {
	calls []checkoutCall
}

func (f *fakeWorktreeCheckout) Checkout(_ context.Context, repoPath string, worktreePath string, commitSHA string) error {
	f.calls = append(f.calls, checkoutCall{repoPath: repoPath, worktreePath: worktreePath, commitSHA: commitSHA})
	return nil
}

func (f *fakeRecoveryRunner) Deploy(_ context.Context, request compose.DeployRequest) (compose.DeployResult, error) {
	f.deploys = append(f.deploys, request)
	return compose.DeployResult{}, f.deployErr
}

func (f *fakeRecoveryRunner) Start(_ context.Context, request compose.LifecycleRequest) error {
	f.starts = append(f.starts, request)
	return f.startErr
}

func (f *fakeRecoveryRunner) Stop(_ context.Context, request compose.LifecycleRequest) error {
	f.stops = append(f.stops, request)
	return f.stopErr
}

func (f *fakeRecoveryRunner) Restart(_ context.Context, request compose.RestartRequest) error {
	f.restarts = append(f.restarts, request)
	return f.restartErr
}

func assertEventTypes(t *testing.T, events []Event, want []string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("events = %#v, want types %#v", events, want)
	}
	for i := range want {
		if events[i].Type != want[i] {
			t.Fatalf("event[%d] type = %q, want %q; events = %#v", i, events[i].Type, want[i], events)
		}
	}
}

func newFakeServiceStore() *fakeServiceStore {
	return &fakeServiceStore{
		apps:                 map[string]App{},
		releases:             map[string]Release{},
		deployments:          map[string]Deployment{},
		appStatuses:          map[string]AppStatus{},
		releaseStatuses:      map[string]ReleaseStatus{},
		deploymentStatuses:   map[string]DeploymentStatus{},
		deploymentFinishedAt: map[string]time.Time{},
		deploymentErrors:     map[string]string{},
		domains:              map[string][]Domain{},
		events:               map[string][]Event{},
	}
}

func writeCurrentCompose(t *testing.T) (string, string) {
	t.Helper()
	worktreePath := t.TempDir()
	composePath := filepath.Join(worktreePath, "compose.yml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write current Compose file: %v", err)
	}
	return worktreePath, composePath
}

func (f *fakeServiceStore) CreateApp(_ context.Context, model App) error {
	f.apps[model.ID] = model
	return nil
}

func (f *fakeServiceStore) GetApp(_ context.Context, id string) (App, error) {
	model, ok := f.apps[id]
	if !ok {
		return App{}, errors.New("not found")
	}
	return model, nil
}

func (f *fakeServiceStore) ListApps(_ context.Context) ([]App, error) {
	apps := make([]App, 0, len(f.apps))
	for _, model := range f.apps {
		apps = append(apps, model)
	}
	return apps, nil
}

func (f *fakeServiceStore) UpdateAppStatus(_ context.Context, id string, status AppStatus, updatedAt time.Time) error {
	model := f.apps[id]
	model.Status = status
	model.UpdatedAt = updatedAt
	f.apps[id] = model
	f.appStatuses[id] = status
	return nil
}

func (f *fakeServiceStore) CreateRelease(_ context.Context, model Release) error {
	f.releases[model.ID] = model
	return nil
}

func (f *fakeServiceStore) GetRelease(_ context.Context, id string) (Release, error) {
	model, ok := f.releases[id]
	if !ok {
		return Release{}, errors.New("not found")
	}
	return model, nil
}

func (f *fakeServiceStore) ListReleasesByApp(_ context.Context, appID string) ([]Release, error) {
	var releases []Release
	for _, model := range f.releases {
		if model.AppID == appID {
			releases = append(releases, model)
		}
	}
	return releases, nil
}

func (f *fakeServiceStore) UpdateReleaseStatus(_ context.Context, id string, status ReleaseStatus, updatedAt time.Time) error {
	model := f.releases[id]
	model.Status = status
	model.UpdatedAt = updatedAt
	f.releases[id] = model
	f.releaseStatuses[id] = status
	return nil
}

func (f *fakeServiceStore) CreateDeployment(_ context.Context, model Deployment) error {
	f.deployments[model.ID] = model
	return nil
}

func (f *fakeServiceStore) UpdateDeploymentStatus(_ context.Context, id string, status DeploymentStatus, finishedAt time.Time, errorMessage string) error {
	f.deploymentStatuses[id] = status
	f.deploymentFinishedAt[id] = finishedAt
	f.deploymentErrors[id] = errorMessage
	model := f.deployments[id]
	model.Status = status
	model.FinishedAt = finishedAt
	model.ErrorMessage = errorMessage
	f.deployments[id] = model
	return nil
}

func (f *fakeServiceStore) UpdateDeploymentFailure(_ context.Context, failure Deployment) error {
	f.deploymentStatuses[failure.ID] = DeploymentStatusFailed
	f.deploymentFinishedAt[failure.ID] = failure.FinishedAt
	f.deploymentErrors[failure.ID] = failure.FailureDetail
	model := f.deployments[failure.ID]
	model.Status = DeploymentStatusFailed
	model.FinishedAt = failure.FinishedAt
	model.FailureStage = failure.FailureStage
	model.FailureDetail = failure.FailureDetail
	model.RetryGuidance = failure.RetryGuidance
	model.ErrorMessage = failure.FailureDetail
	f.deployments[failure.ID] = model
	return nil
}

func (f *fakeServiceStore) AttachDomain(_ context.Context, model Domain) error {
	f.domains[model.AppID] = append(f.domains[model.AppID], model)
	return nil
}

func (f *fakeServiceStore) ListDomainsByApp(_ context.Context, appID string) ([]Domain, error) {
	return f.domains[appID], nil
}

func (f *fakeServiceStore) CreateEvent(_ context.Context, model Event) error {
	f.events[model.AppID] = append(f.events[model.AppID], model)
	return nil
}

func (f *fakeServiceStore) ListEventsByApp(_ context.Context, appID string) ([]Event, error) {
	return f.events[appID], nil
}

package app

import (
	"context"
	"errors"
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

func TestServiceRollbackReleaseStartsDeploymentForKnownRelease(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	runner := &fakeRecoveryRunner{}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusFailed}
	store.releases["rel_1"] = Release{
		ID:          "rel_1",
		AppID:       "app_1",
		CommitSHA:   "abc123",
		ComposePath: "compose.yml",
		Status:      ReleaseStatusSucceeded,
	}

	deployment, err := service.RollbackRelease(ctx, "app_1", "rel_1", "dep_rollback_1")
	if err != nil {
		t.Fatalf("RollbackRelease: %v", err)
	}

	if deployment.AppID != "app_1" {
		t.Fatalf("AppID = %q", deployment.AppID)
	}
	if deployment.ReleaseID != "rel_1" {
		t.Fatalf("ReleaseID = %q", deployment.ReleaseID)
	}
	if deployment.Status != DeploymentStatusSucceeded {
		t.Fatalf("Status = %q", deployment.Status)
	}
	if !deployment.StartedAt.Equal(now) {
		t.Fatalf("StartedAt = %s", deployment.StartedAt)
	}
	if len(runner.deploys) != 1 {
		t.Fatalf("deploy requests = %#v", runner.deploys)
	}
	request := runner.deploys[0]
	if request.AppName != "app_1" || request.ReleaseID != "rel_1" || request.CommitSHA != "abc123" || request.ComposePath != "compose.yml" || request.ProjectDir != "/apps/app_1/worktree" {
		t.Fatalf("deploy request = %#v", request)
	}
	if store.appStatuses["app_1"] != AppStatusHealthy {
		t.Fatalf("app status = %q", store.appStatuses["app_1"])
	}
	if store.releaseStatuses["rel_1"] != ReleaseStatusRolledBack {
		t.Fatalf("release status = %q", store.releaseStatuses["rel_1"])
	}
	if store.deploymentStatuses["dep_rollback_1"] != DeploymentStatusSucceeded {
		t.Fatalf("deployment status = %q", store.deploymentStatuses["dep_rollback_1"])
	}
	assertEventTypes(t, store.events["app_1"], []string{"rollback.triggered", "rollback.succeeded"})
}

func TestServiceRollbackReleaseChecksOutSelectedCommitBeforeDeploy(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	runner := &fakeRecoveryRunner{}
	checkout := &fakeWorktreeCheckout{}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithWorktreeCheckout(checkout))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", RepoPath: "/apps/app_1/repo.git", WorktreePath: "/apps/app_1/worktree", Status: AppStatusFailed}
	store.releases["rel_good"] = Release{ID: "rel_good", AppID: "app_1", CommitSHA: "good", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	_, err := service.RollbackRelease(ctx, "app_1", "rel_good", "dep_rollback_1")
	if err != nil {
		t.Fatalf("RollbackRelease: %v", err)
	}
	if len(checkout.calls) != 1 {
		t.Fatalf("checkout calls = %#v", checkout.calls)
	}
	if checkout.calls[0] != (checkoutCall{repoPath: "/apps/app_1/repo.git", worktreePath: "/apps/app_1/worktree", commitSHA: "good"}) {
		t.Fatalf("checkout call = %#v", checkout.calls[0])
	}
	if len(runner.deploys) != 1 {
		t.Fatalf("deploys = %#v", runner.deploys)
	}
}

func TestServiceRollbackRejectsReleaseFromDifferentApp(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	service := NewService(store)
	store.releases["rel_1"] = Release{ID: "rel_1", AppID: "other-app"}

	_, err := service.RollbackRelease(ctx, "app_1", "rel_1", "dep_rollback_1")
	if err == nil {
		t.Fatal("RollbackRelease error = nil, want error")
	}
}

func TestServiceRestartAppAndServiceUseLatestSuccessfulRelease(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	runner := &fakeRecoveryRunner{}
	service := NewService(store, WithClock(func() time.Time { return now }), WithRecoveryRunner(runner))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusHealthy}
	store.releases["rel_bad"] = Release{ID: "rel_bad", AppID: "app_1", CommitSHA: "bad", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusFailed, CreatedAt: now.Add(-time.Hour)}
	store.releases["rel_good"] = Release{ID: "rel_good", AppID: "app_1", CommitSHA: "good", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

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
	if appRestart.AppName != "app_1" || appRestart.ProjectDir != "/apps/app_1/worktree" || appRestart.ComposePath != "/apps/app_1/worktree/compose.yml" || appRestart.ServiceName != "" {
		t.Fatalf("app restart request = %#v", appRestart)
	}
	serviceRestart := runner.restarts[1]
	if serviceRestart.ServiceName != "web" {
		t.Fatalf("service restart request = %#v", serviceRestart)
	}
	assertEventTypes(t, store.events["app_1"], []string{"restart.triggered", "restart.succeeded", "service.restart.triggered", "service.restart.succeeded"})
}

func TestServiceRedeployCurrentMainUsesFailedCurrentCommitInsteadOfLatestGood(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
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
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", RepoPath: "/apps/app_1/repo.git", WorktreePath: "/apps/app_1/worktree", Status: AppStatusFailed}
	store.releases["rel_old"] = Release{ID: "rel_old", AppID: "app_1", CommitSHA: "old", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}
	store.releases["rel_new"] = Release{ID: "rel_new", AppID: "app_1", CommitSHA: "new", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusFailed, CreatedAt: now.Add(-time.Hour)}

	// When
	deployment, err := service.RedeployCurrentMain(ctx, "app_1", "dep_redeploy_1")

	// Then
	if err != nil {
		t.Fatalf("RedeployCurrentMain: %v", err)
	}
	if deployment.ReleaseID != "rel_new" || deployment.CommitSHA != "new" {
		t.Fatalf("deployment = %#v, want current main release", deployment)
	}
	if len(runner.deploys) != 1 || runner.deploys[0].CommitSHA != "new" {
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
	runner := &fakeRecoveryRunner{}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusHealthy}
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
	if request.ReleaseID != "rel_new" || request.CommitSHA != "new" || request.ProjectDir != "/apps/app_1/worktree" {
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
	runner := &fakeRecoveryRunner{}
	resolver := &fakeConfigResolver{env: map[string]string{"DATABASE_URL": "postgres://secret"}}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithConfigResolver(resolver), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusHealthy}
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

func TestServiceRollbackPassesResolvedConfigEnvironment(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	runner := &fakeRecoveryRunner{}
	resolver := &fakeConfigResolver{env: map[string]string{"API_TOKEN": "secret"}}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithConfigResolver(resolver))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusHealthy}
	store.releases["rel_good"] = Release{ID: "rel_good", AppID: "app_1", CommitSHA: "good", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	if _, err := service.RollbackRelease(ctx, "app_1", "rel_good", "dep_rollback_1"); err != nil {
		t.Fatalf("RollbackRelease: %v", err)
	}
	if len(runner.deploys) != 1 || runner.deploys[0].Env["API_TOKEN"] != "secret" {
		t.Fatalf("deploy requests = %#v", runner.deploys)
	}
}

func TestServiceRedeployConfigFailureStopsBeforeCompose(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	runner := &fakeRecoveryRunner{}
	resolver := &fakeConfigResolver{err: errors.New("missing required config for app_1: SECRET")}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithConfigResolver(resolver), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusHealthy}
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
	deployFailure := errors.New("compose failed")
	failure := compose.NewDeployError(compose.DeployStageBuildServices, deployFailure)
	runner := &fakeRecoveryRunner{deployErr: failure}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusHealthy}
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
	failure := errors.New("docker output included postgres://secret")
	runner := &fakeRecoveryRunner{deployErr: failure}
	resolver := &fakeConfigResolver{env: map[string]string{"DATABASE_URL": "postgres://secret"}}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithConfigResolver(resolver), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusHealthy}
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

func TestServiceRecoverDeployedAppsRedeploysLatestGoodRelease(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	runner := &fakeRecoveryRunner{}
	checkout := &fakeWorktreeCheckout{}
	service := NewService(
		store,
		WithClock(func() time.Time { return now }),
		WithRecoveryRunner(runner),
		WithWorktreeCheckout(checkout),
		WithDeploymentIDGenerator(func() (string, error) {
			return "dep_startup_attempt", nil
		}),
	)
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", RepoPath: "/apps/app_1/repo.git", WorktreePath: "/apps/app_1/worktree", Status: AppStatusHealthy}
	store.apps["empty"] = App{ID: "empty", Name: "empty", RepoPath: "/apps/empty/repo.git", WorktreePath: "/apps/empty/worktree", Status: AppStatusCreated}
	store.releases["rel_old"] = Release{ID: "rel_old", AppID: "app_1", CommitSHA: "old", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now.Add(-time.Hour)}
	store.releases["rel_failed"] = Release{ID: "rel_failed", AppID: "app_1", CommitSHA: "failed", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusFailed, CreatedAt: now}

	if err := service.RecoverDeployedApps(ctx); err != nil {
		t.Fatalf("RecoverDeployedApps: %v", err)
	}
	if len(checkout.calls) != 1 {
		t.Fatalf("checkout calls = %#v", checkout.calls)
	}
	if checkout.calls[0] != (checkoutCall{repoPath: "/apps/app_1/repo.git", worktreePath: "/apps/app_1/worktree", commitSHA: "old"}) {
		t.Fatalf("checkout call = %#v", checkout.calls[0])
	}
	if len(runner.deploys) != 1 {
		t.Fatalf("deploy requests = %#v", runner.deploys)
	}
	if runner.deploys[0].ReleaseID != "rel_old" || runner.deploys[0].CommitSHA != "old" {
		t.Fatalf("deploy request = %#v", runner.deploys[0])
	}
	if store.deploymentStatuses["dep_startup_attempt"] != DeploymentStatusSucceeded {
		t.Fatalf("deployment statuses = %#v", store.deploymentStatuses)
	}
	if store.deployments["dep_startup_attempt"].Trigger != DeploymentTriggerStartupRecovery {
		t.Fatalf("startup deployment = %#v", store.deployments["dep_startup_attempt"])
	}
	for index, want := range []string{"Startup recovery for release rel_old started", "Startup recovery for release rel_old succeeded"} {
		if got := store.events["app_1"][index].Message; got != want {
			t.Fatalf("event[%d] message = %q, want %q", index, got, want)
		}
	}
}

func TestServiceRecoverDeployedAppsDoesNotRecommendCurrentMainRedeploy(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	deployFailure := errors.New("compose failed")
	service := NewService(
		store,
		WithClock(func() time.Time { return now }),
		WithRecoveryRunner(&fakeRecoveryRunner{deployErr: deployFailure}),
		WithDeploymentIDGenerator(func() (string, error) { return "dep_startup_attempt", nil }),
	)
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusHealthy}
	store.releases["rel_good"] = Release{ID: "rel_good", AppID: "app_1", CommitSHA: "good", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	err := service.RecoverDeployedApps(ctx)

	if !errors.Is(err, deployFailure) {
		t.Fatalf("RecoverDeployedApps error = %v, want wrapped %v", err, deployFailure)
	}
	guidance := store.deployments["dep_startup_attempt"].RetryGuidance
	if !strings.Contains(guidance, "restart sshdockd") || strings.Contains(guidance, "apps redeploy") {
		t.Fatalf("startup retry guidance = %q, want daemon-restart guidance without current-main redeploy", guidance)
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

type fakeRecoveryRunner struct {
	deploys  []compose.DeployRequest
	restarts []compose.RestartRequest

	deployErr  error
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

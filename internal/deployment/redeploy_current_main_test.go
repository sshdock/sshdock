package deployment_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/deployment"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

func TestRedeployCurrentMainSelectsCommitAndReusesRelease(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "unrecorded current commit"
		if existing {
			name = "failed current release"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db, model := redeployTestApp(t)
			now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
			old := app.Release{ID: "rel_old", AppID: model.ID, CommitSHA: "old", ComposePath: "/historical/compose.yml", Status: app.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now}
			if err := db.CreateRelease(ctx, old); err != nil {
				t.Fatal(err)
			}
			releaseID := app.ReleaseID(model.ID, "current")
			if existing {
				releaseID = "rel_current"
				if err := db.CreateRelease(ctx, app.Release{ID: releaseID, AppID: model.ID, CommitSHA: "current", ComposePath: "/old/worktree/compose.yml", Status: app.ReleaseStatusFailed, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}); err != nil {
					t.Fatal(err)
				}
			}
			runner := &compose.FakeRunner{DeployOutput: "building current commit\n"}
			resolver := &redeployConfigResolver{env: map[string]string{"DATABASE_URL": "postgres://secret"}}
			checkoutCount := 0
			main := "current"
			currentMain := app.CurrentMainResolverFunc(func(_ context.Context, repoPath string) (string, error) {
				if repoPath != model.RepoPath {
					t.Fatalf("resolve repo path = %q, want %q", repoPath, model.RepoPath)
				}
				return main, nil
			})
			executor := newRedeployExecutor(t, deployment.Config{
				Store: db, Runner: runner, ConfigResolver: resolver, Now: func() time.Time { return now },
				Checkout: redeployCheckoutFunc(func(_ context.Context, repoPath, worktreePath, commitSHA string) error {
					checkoutCount++
					if repoPath != model.RepoPath || worktreePath != model.WorktreePath || commitSHA != "current" {
						t.Fatalf("checkout = %q %q %q", repoPath, worktreePath, commitSHA)
					}
					return os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte(redeployCompose), 0o644)
				}),
			})
			if err := db.CreateDeployment(ctx, app.Deployment{ID: "dep_previous", AppID: model.ID, ReleaseID: old.ID, CommitSHA: old.CommitSHA, Trigger: app.DeploymentTriggerRedeploy, Status: app.DeploymentStatusSucceeded, StartedAt: now}); err != nil {
				t.Fatal(err)
			}
			producer := deployment.NewRedeployer(deployment.RedeployConfig{Store: db, CurrentMainResolver: currentMain, Poll: func(ctx context.Context) error {
				main = "newer"
				_ = redeployRunQueued(t, ctx, db, executor)
				return nil
			}})
			for _, id := range []string{"dep_first", "dep_retry"} {
				main = "current"
				if err := producer.RedeployCurrentMain(ctx, model.ID, id); err != nil {
					t.Fatalf("RedeployCurrentMain: %v", err)
				}
				attempt := redeployAttempt(t, db, model.ID, id)
				if attempt.ReleaseID != releaseID || attempt.CommitSHA != "current" || attempt.Trigger != app.DeploymentTriggerRedeploy || attempt.Status != app.DeploymentStatusSucceeded || !attempt.FinishedAt.Equal(now) {
					t.Fatalf("deployment = %#v", attempt)
				}
				log, err := db.DeploymentLog(ctx, model.ID, id)
				if err != nil || !strings.Contains(log.Content, "building current commit") || !strings.Contains(log.Content, "deploy: succeeded") {
					t.Fatalf("deployment log = %#v, error = %v", log, err)
				}
			}
			if checkoutCount != 2 || len(runner.DeployRequests) != 2 || resolver.calls != 2 {
				t.Fatalf("checkouts = %d, deploys = %d, config resolutions = %d", checkoutCount, len(runner.DeployRequests), resolver.calls)
			}
			for _, request := range runner.DeployRequests {
				if request.CommitSHA != "current" || request.ProjectDir != model.WorktreePath || request.ComposePath != filepath.Join(model.WorktreePath, "compose.yml") || request.Env["DATABASE_URL"] != "postgres://secret" || request.Env[compose.GitSHAEnv] != "current" {
					t.Fatalf("deploy request = %#v", request)
				}
			}
			releases, err := db.ListReleasesByApp(ctx, model.ID)
			if err != nil || len(releases) != 2 {
				t.Fatalf("releases = %#v, error = %v", releases, err)
			}
			release, err := db.GetRelease(ctx, releaseID)
			if err != nil || release.Status != app.ReleaseStatusSucceeded || (!existing && release.ComposePath != filepath.Join(model.WorktreePath, "compose.yml")) {
				t.Fatalf("current release = %#v, error = %v", release, err)
			}
			previous, err := db.GetRelease(ctx, old.ID)
			if err != nil || previous != old {
				t.Fatalf("previous successful release changed: %#v, error = %v", previous, err)
			}
			updatedApp, err := db.GetApp(ctx, model.ID)
			if err != nil || updatedApp.Status != app.AppStatusHealthy {
				t.Fatalf("app = %#v, error = %v", updatedApp, err)
			}
			events, err := db.ListEventsByApp(ctx, model.ID)
			if err != nil {
				t.Fatal(err)
			}
			started, succeeded, queued := 0, 0, 0
			for _, event := range events {
				switch event.Type {
				case "git.ref_accepted":
					t.Fatalf("redeploy invented Git acceptance: %#v", event)
				case "deploy.queued":
					queued++
				case "deploy.started":
					started++
				case "deploy.succeeded":
					succeeded++
				case "deploy.failed":
					t.Fatalf("unexpected failure event: %#v", event)
				}
			}
			if started != 2 || succeeded != 2 || queued != 2 {
				t.Fatalf("deployment events = %#v", events)
			}
		})
	}
}

func TestRedeployCurrentMainPersistsPreflightFailures(t *testing.T) {
	checkoutErr := errors.New("checkout failed")
	configErr := errors.New("missing required config: SECRET")
	for _, test := range []struct {
		name     string
		stage    string
		checkout error
		compose  string
		config   error
		cause    error
	}{
		{name: "checkout", stage: "checkout", checkout: checkoutErr, cause: checkoutErr},
		{name: "missing compose", stage: "detect compose"},
		{name: "config", stage: "config", compose: redeployCompose, config: configErr, cause: configErr},
		{name: "invalid compose", stage: "validate compose", compose: "services: ["},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db, model := redeployTestApp(t)
			runner := &compose.FakeRunner{}
			executor := newRedeployExecutor(t, deployment.Config{
				Store: db, Runner: runner,
				ConfigResolver: &redeployConfigResolver{err: test.config},
				Checkout: redeployCheckoutFunc(func(context.Context, string, string, string) error {
					if test.checkout != nil {
						return test.checkout
					}
					if test.compose != "" {
						return os.WriteFile(filepath.Join(model.WorktreePath, "compose.yml"), []byte(test.compose), 0o644)
					}
					return nil
				}),
			})
			producer := redeployProducer(t, db, executor, redeployMain("current"))
			err := producer.RedeployCurrentMain(ctx, model.ID, "dep_failed")
			if err == nil || (test.cause != nil && !strings.Contains(err.Error(), test.cause.Error())) {
				t.Fatalf("RedeployCurrentMain error = %v, want %v", err, test.cause)
			}
			attempt := redeployAttempt(t, db, model.ID, "dep_failed")
			if attempt.Status != app.DeploymentStatusFailed || attempt.FailureStage != test.stage || attempt.FailureDetail == "" || attempt.RetryGuidance == "" || attempt.FinishedAt.IsZero() {
				t.Fatalf("failed attempt = %#v", attempt)
			}
			if len(runner.DeployRequests) != 0 {
				t.Fatalf("Compose ran after %s failure", test.stage)
			}
			updatedApp, err := db.GetApp(ctx, model.ID)
			if err != nil || updatedApp.Status != app.AppStatusFailed {
				t.Fatalf("app = %#v, error = %v", updatedApp, err)
			}
			releases, err := db.ListReleasesByApp(ctx, model.ID)
			if err != nil {
				t.Fatal(err)
			}
			if test.stage == "checkout" || test.stage == "detect compose" {
				if len(releases) != 0 {
					t.Fatalf("release created before Compose detection: %#v", releases)
				}
			} else if len(releases) != 1 || releases[0].Status != app.ReleaseStatusFailed {
				t.Fatalf("failed releases = %#v", releases)
			}
		})
	}
}

func TestRedeployFailureRedactsSecretsAndPreservesReleaseSuccess(t *testing.T) {
	for _, status := range []app.ReleaseStatus{app.ReleaseStatusFailed, app.ReleaseStatusSucceeded} {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			db, model := redeployTestApp(t)
			now := time.Now().UTC()
			release := app.Release{ID: "rel_existing", AppID: model.ID, CommitSHA: "current", Status: status, CreatedAt: now, UpdatedAt: now}
			if err := db.CreateRelease(ctx, release); err != nil {
				t.Fatal(err)
			}
			secret := "postgres://runtime-secret"
			historicalSecret := "historical-secret"
			cause := errors.New("docker output included " + secret + " and " + historicalSecret)
			runner := &compose.FakeRunner{DeployErr: compose.NewDeployError(compose.DeployStageBuildServices, cause), DeployOutput: cause.Error() + "\n"}
			executor := newRedeployExecutor(t, deployment.Config{
				Store: db, Runner: runner, Checkout: redeployComposeCheckout(model.WorktreePath),
				ConfigResolver: redeployRedactingConfig{env: map[string]string{"DATABASE_URL": secret}, values: map[string]string{"DATABASE_URL": secret, "old/TOKEN": historicalSecret}},
			})
			producer := redeployProducer(t, db, executor, redeployMain("current"))
			err := producer.RedeployCurrentMain(ctx, model.ID, "dep_retry")
			if err == nil || !strings.Contains(err.Error(), "docker output included <redacted>") || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), historicalSecret) || !strings.Contains(err.Error(), "<redacted>") {
				t.Fatalf("returned failure = %v", err)
			}
			attempt := redeployAttempt(t, db, model.ID, "dep_retry")
			if attempt.Status != app.DeploymentStatusFailed || attempt.FailureStage != "build services" || !strings.Contains(attempt.FailureDetail, "fix=fix Dockerfile or build context errors") || !strings.Contains(attempt.RetryGuidance, "sudo sshdock apps redeploy "+model.ID) {
				t.Fatalf("failed attempt = %#v", attempt)
			}
			events, err := db.ListEventsByApp(ctx, model.ID)
			if err != nil {
				t.Fatal(err)
			}
			log, err := db.DeploymentLog(ctx, model.ID, "dep_retry")
			if err != nil {
				t.Fatal(err)
			}
			allOutput := attempt.FailureDetail + "\n" + log.Content
			failed := false
			for _, event := range events {
				allOutput += "\n" + event.Message
				failed = failed || event.Type == "deploy.failed"
			}
			if !failed || strings.Contains(allOutput, secret) || strings.Contains(allOutput, historicalSecret) || !strings.Contains(log.Content, "<redacted>") {
				t.Fatalf("persisted output leaked or missed failure:\n%s", allOutput)
			}
			stored, err := db.GetRelease(ctx, release.ID)
			if err != nil || stored.Status != status {
				t.Fatalf("release = %#v, error = %v; want status %q", stored, err, status)
			}
		})
	}
}

func TestRedeployExecutionReturnsFailurePersistenceErrors(t *testing.T) {
	ctx := context.Background()
	db, model := redeployTestApp(t)
	deploymentErr := errors.New("deployment failure was not stored")
	releaseErr := errors.New("release failure was not stored")
	cause := errors.New("compose failed")
	faults := redeployFailureStore{SQLiteStore: db, deploymentErr: deploymentErr, releaseErr: releaseErr}
	executor := newRedeployExecutor(t, deployment.Config{
		Store: faults, Runner: &compose.FakeRunner{DeployErr: cause},
		Checkout: redeployComposeCheckout(model.WorktreePath),
	})
	attempt := app.Deployment{ID: "dep_failed", AppID: model.ID, ReleaseID: app.ReleaseID(model.ID, "current"), CommitSHA: "current", Trigger: app.DeploymentTriggerRedeploy, Status: app.DeploymentStatusPending, StartedAt: time.Now().UTC()}
	if err := db.QueueRedeployment(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	err := redeployRunQueued(t, ctx, db, executor)
	for _, want := range []error{cause, deploymentErr, releaseErr} {
		if !errors.Is(err, want) {
			t.Fatalf("RedeployCurrentMain error = %v, want wrapped %v", err, want)
		}
	}
}

func TestRedeployCurrentMainDoesNotAdmitAttemptWithoutCurrentMain(t *testing.T) {
	cause := errors.New("main is missing")
	for _, resolver := range []app.CurrentMainResolver{nil, app.CurrentMainResolverFunc(func(context.Context, string) (string, error) { return "", cause })} {
		ctx := context.Background()
		db, model := redeployTestApp(t)
		producer := deployment.NewRedeployer(deployment.RedeployConfig{Store: db, CurrentMainResolver: resolver})
		err := producer.RedeployCurrentMain(ctx, model.ID, "dep_unadmitted")
		if err == nil || !strings.Contains(err.Error(), "resolve remote main") || (resolver != nil && !errors.Is(err, cause)) {
			t.Fatalf("RedeployCurrentMain error = %v", err)
		}
		attempts, err := db.ListDeploymentsByApp(ctx, model.ID)
		if err != nil || len(attempts) != 0 {
			t.Fatalf("unadmitted attempts = %#v, error = %v", attempts, err)
		}
	}
}

const redeployCompose = "services:\n  web:\n    image: nginx:alpine\n"

func redeployTestApp(t *testing.T) (*store.SQLiteStore, app.App) {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().UTC()
	model := app.App{ID: "my-app", Name: "my-app", NodeID: "local", RepoPath: filepath.Join(t.TempDir(), "repo.git"), WorktreePath: t.TempDir(), Status: app.AppStatusFailed, CreatedAt: now, UpdatedAt: now}
	if err := db.CreateApp(ctx, model); err != nil {
		t.Fatal(err)
	}
	return db, model
}

func redeployAttempt(t *testing.T, db *store.SQLiteStore, appID, deploymentID string) app.Deployment {
	t.Helper()
	attempts, err := db.ListDeploymentsByApp(context.Background(), appID)
	if err != nil {
		t.Fatal(err)
	}
	for _, attempt := range attempts {
		if attempt.ID == deploymentID {
			return attempt
		}
	}
	t.Fatalf("deployment %q was not persisted: %#v", deploymentID, attempts)
	return app.Deployment{}
}

func redeployMain(commit string) app.CurrentMainResolver {
	return app.CurrentMainResolverFunc(func(context.Context, string) (string, error) { return commit, nil })
}

type redeployCheckoutFunc func(context.Context, string, string, string) error

func (f redeployCheckoutFunc) Checkout(ctx context.Context, repoPath, worktreePath, commitSHA string) error {
	return f(ctx, repoPath, worktreePath, commitSHA)
}

func redeployComposeCheckout(worktreePath string) redeployCheckoutFunc {
	return redeployCheckoutFunc(func(context.Context, string, string, string) error {
		return os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte(redeployCompose), 0o644)
	})
}

type redeployConfigResolver struct {
	env   map[string]string
	err   error
	calls int
}

func (r *redeployConfigResolver) ResolveEnv(context.Context, string) (map[string]string, error) {
	r.calls++
	return r.env, r.err
}

type redeployRedactingConfig struct {
	env    map[string]string
	values map[string]string
}

func (r redeployRedactingConfig) ResolveEnv(context.Context, string) (map[string]string, error) {
	return r.env, nil
}

func (r redeployRedactingConfig) RedactionValues(context.Context, string) (map[string]string, error) {
	return r.values, nil
}

type redeployFailureStore struct {
	*store.SQLiteStore
	deploymentErr error
	releaseErr    error
}

func (s redeployFailureStore) UpdateDeploymentFailure(context.Context, app.Deployment) error {
	return s.deploymentErr
}

func (s redeployFailureStore) MarkReleaseFailedUnlessSucceeded(context.Context, string, time.Time) error {
	return s.releaseErr
}

func redeployProducer(t *testing.T, db *store.SQLiteStore, executor *deployment.Executor, resolver app.CurrentMainResolver) *deployment.Redeployer {
	t.Helper()
	return deployment.NewRedeployer(deployment.RedeployConfig{Store: db, CurrentMainResolver: resolver, Poll: func(ctx context.Context) error {
		_ = redeployRunQueued(t, ctx, db, executor)
		return nil
	}})
}

func redeployRunQueued(t *testing.T, ctx context.Context, db *store.SQLiteStore, executor *deployment.Executor) error {
	t.Helper()
	attempt, found, err := db.ClaimNextPendingDeployment(ctx)
	if err != nil || !found {
		t.Fatalf("ClaimNextPendingDeployment: found=%t, error=%v", found, err)
	}
	return executor.Execute(ctx, attempt)
}

func TestRedeployCancellationLeavesFixedAttemptForDaemon(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, model := redeployTestApp(t)
	queued := make(chan struct{})
	producer := deployment.NewRedeployer(deployment.RedeployConfig{
		Store: redeploySignalingStore{SQLiteStore: db, queued: queued}, CurrentMainResolver: redeployMain("queued-commit"),
	})
	done := make(chan error, 1)
	go func() { done <- producer.RedeployCurrentMain(ctx, model.ID, "dep_detached") }()
	select {
	case <-queued:
	case <-time.After(2 * time.Second):
		t.Fatal("redeploy did not enter durable queue")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "daemon deployment continues") || !strings.Contains(err.Error(), "sudo sshdock deployments logs "+model.ID+" dep_detached -f") {
			t.Fatalf("detached redeploy error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("redeploy did not stop waiting after cancellation")
	}
	attempt := redeployAttempt(t, db, model.ID, "dep_detached")
	if attempt.Status != app.DeploymentStatusPending || attempt.CommitSHA != "queued-commit" || !attempt.FinishedAt.IsZero() {
		t.Fatalf("cancelled client changed queued attempt = %#v", attempt)
	}
	if _, err := db.DeploymentLog(context.Background(), model.ID, attempt.ID); err != nil {
		t.Fatalf("durable queue log missing: %v", err)
	}
	runner := &compose.FakeRunner{}
	executor := newRedeployExecutor(t, deployment.Config{Store: db, Runner: runner, Checkout: redeployComposeCheckout(model.WorktreePath)})
	if err := redeployRunQueued(t, context.Background(), db, executor); err != nil {
		t.Fatalf("daemon execution after client cancellation: %v", err)
	}
	if len(runner.DeployRequests) != 1 || runner.DeployRequests[0].CommitSHA != "queued-commit" || runner.DeployRequests[0].Env[compose.GitSHAEnv] != "queued-commit" {
		t.Fatalf("daemon deployed wrong revision: %#v", runner.DeployRequests)
	}
	if completed := redeployAttempt(t, db, model.ID, attempt.ID); completed.Status != app.DeploymentStatusSucceeded {
		t.Fatalf("daemon did not complete detached attempt: %#v", completed)
	}
}

func TestRedeployWaitReadFailureKeepsQueuedAttempt(t *testing.T) {
	ctx := context.Background()
	db, model := redeployTestApp(t)
	cause := errors.New("cannot read deployment history")
	producer := deployment.NewRedeployer(deployment.RedeployConfig{Store: redeployReadFailureStore{SQLiteStore: db, err: cause}, CurrentMainResolver: redeployMain("current")})
	err := producer.RedeployCurrentMain(ctx, model.ID, "dep_unobserved")
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "deployments logs "+model.ID+" dep_unobserved -f") {
		t.Fatalf("attachment error = %v", err)
	}
	if attempt := redeployAttempt(t, db, model.ID, "dep_unobserved"); attempt.Status != app.DeploymentStatusPending {
		t.Fatalf("observation failure changed attempt = %#v", attempt)
	}
}

type redeploySignalingStore struct {
	*store.SQLiteStore
	queued chan struct{}
}

func (s redeploySignalingStore) QueueRedeployment(ctx context.Context, attempt app.Deployment) error {
	if err := s.SQLiteStore.QueueRedeployment(ctx, attempt); err != nil {
		return err
	}
	close(s.queued)
	return nil
}

type redeployReadFailureStore struct {
	*store.SQLiteStore
	err error
}

func (s redeployReadFailureStore) ListDeploymentsByApp(context.Context, string) ([]app.Deployment, error) {
	return nil, s.err
}

func newRedeployExecutor(t *testing.T, cfg deployment.Config) *deployment.Executor {
	t.Helper()
	if cfg.Routing == nil {
		cfg.Routing = router.NewService(router.ServiceConfig{Store: cfg.Store.(store.Store), Router: router.NewFakeRouter(), LocksDir: t.TempDir(), Now: cfg.Now})
	}
	return deployment.NewExecutor(cfg)
}

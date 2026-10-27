package deployment

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

func TestExecutorFailureAttemptsAllTerminalWrites(t *testing.T) {
	ctx := context.Background()
	sqlite, checkout := newPersistenceFixture(t)
	deployErr := errors.New("compose failed")
	deploymentErr := errors.New("deployment failure persistence failed")
	releaseErr := errors.New("release failure persistence failed")
	wrapped := &persistenceFailureStore{SQLiteStore: sqlite, deploymentErr: deploymentErr, releaseErr: releaseErr}
	executor := newTestExecutor(t, Config{Store: wrapped, Checkout: checkout, Runner: &compose.FakeRunner{DeployErr: deployErr}})

	err := executeAttempt(t, ctx, sqlite, executor, "my-app", "current", "dep_failure")

	for _, want := range []error{deployErr, deploymentErr, releaseErr} {
		if !errors.Is(err, want) {
			t.Fatalf("Execute = %v, want %v", err, want)
		}
	}
	model, err := sqlite.GetApp(ctx, "my-app")
	if err != nil || model.Status != app.AppStatusFailed {
		t.Fatalf("app = %#v, %v; failure persistence stopped after the first error", model, err)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil || len(events) != 3 || events[0].Type != "deploy.queued" || events[2].Type != "deploy.failed" {
		t.Fatalf("events = %#v, %v", events, err)
	}
}

func TestExecutorSuccessLogFailureStillFinishesAttempt(t *testing.T) {
	ctx := context.Background()
	sqlite, checkout := newPersistenceFixture(t)
	logErr := errors.New("success log persistence failed")
	wrapped := &persistenceFailureStore{SQLiteStore: sqlite, successLogErr: logErr}
	executor := newTestExecutor(t, Config{Store: wrapped, Checkout: checkout, Runner: &compose.FakeRunner{}})

	err := executeAttempt(t, ctx, sqlite, executor, "my-app", "current", "dep_success")

	if !errors.Is(err, logErr) {
		t.Fatalf("Execute = %v, want %v", err, logErr)
	}
	deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if err != nil || len(deployments) != 1 || deployments[0].Status != app.DeploymentStatusSucceeded {
		t.Fatalf("deployments = %#v, %v; log failure left deployment active", deployments, err)
	}
	model, err := sqlite.GetApp(ctx, "my-app")
	if err != nil || model.Status != app.AppStatusHealthy {
		t.Fatalf("app = %#v, %v", model, err)
	}
}

func TestRedeployReturnsPersistedErrorAfterSuccessfulContainers(t *testing.T) {
	ctx := context.Background()
	sqlite, checkout := newPersistenceFixture(t)
	logErr := errors.New("success log persistence failed")
	wrapped := &persistenceFailureStore{SQLiteStore: sqlite, successLogErr: logErr}
	runner := &compose.FakeRunner{}
	executor := newTestExecutor(t, Config{Store: wrapped, Checkout: checkout, Runner: runner})
	var executionErr error
	producer := NewRedeployer(RedeployConfig{Store: sqlite, CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) { return "current", nil }), Poll: func(ctx context.Context) error {
		attempt, found, err := sqlite.ClaimNextPendingDeployment(ctx)
		if err != nil || !found {
			t.Fatalf("ClaimNextPendingDeployment = %#v, %v, %v", attempt, found, err)
		}
		executionErr = executor.Execute(ctx, attempt)
		return nil // The waiting client observes the daemon's durable outcome.
	}})

	err := producer.RedeployCurrentMain(ctx, "my-app", "dep_success")
	if err == nil || !strings.Contains(err.Error(), logErr.Error()) || !strings.Contains(err.Error(), "dep_success") {
		t.Fatalf("RedeployCurrentMain = %v, want persisted success-log error", err)
	}
	if !errors.Is(executionErr, logErr) {
		t.Fatalf("Execute = %v, want %v", executionErr, logErr)
	}
	attempts, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if err != nil || len(attempts) != 1 || attempts[0].Status != app.DeploymentStatusSucceeded || !strings.Contains(attempts[0].ErrorMessage, logErr.Error()) || !strings.Contains(attempts[0].ErrorMessage, "containers deployed") {
		t.Fatalf("attempts = %#v, %v; want successful containers with actionable persistence error", attempts, err)
	}
	model, err := sqlite.GetApp(ctx, "my-app")
	if err != nil || model.Status != app.AppStatusHealthy || len(runner.DeployRequests) != 1 {
		t.Fatalf("app = %#v, %v, deploy requests = %#v", model, err, runner.DeployRequests)
	}
	release, err := sqlite.GetRelease(ctx, attempts[0].ReleaseID)
	if err != nil || release.Status != app.ReleaseStatusSucceeded {
		t.Fatalf("release = %#v, %v; want successful runtime outcome preserved", release, err)
	}
}

func TestExecutorRedeployFailureKeepsReleaseHistory(t *testing.T) {
	for _, status := range []app.ReleaseStatus{app.ReleaseStatusFailed, app.ReleaseStatusSucceeded} {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			sqlite, checkout := newPersistenceFixture(t)
			releaseID := app.ReleaseID("my-app", "current")
			now := time.Now().UTC()
			if err := sqlite.CreateRelease(ctx, app.Release{ID: releaseID, AppID: "my-app", CommitSHA: "current", Status: status, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			executor := newTestExecutor(t, Config{Store: sqlite, Checkout: checkout, Runner: &compose.FakeRunner{DeployErr: errors.New("retry failed")}})

			if err := executeAttempt(t, ctx, sqlite, executor, "my-app", "current", "dep_retry"); err == nil {
				t.Fatal("Execute succeeded")
			}
			release, err := sqlite.GetRelease(ctx, releaseID)
			if err != nil || release.Status != status {
				t.Fatalf("release = %#v, %v; want historical status %s", release, err, status)
			}
		})
	}
}

func TestExecutorCancellationStillRecordsTerminalFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sqlite, checkout := newPersistenceFixture(t)
	runner := &persistenceCancellingRunner{FakeRunner: &compose.FakeRunner{}, cancel: cancel}
	executor := newTestExecutor(t, Config{Store: sqlite, Checkout: checkout, Runner: runner})

	if err := executeAttempt(t, ctx, sqlite, executor, "my-app", "current", "dep_cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute = %v, want context cancellation", err)
	}
	deployments, err := sqlite.ListDeploymentsByApp(context.Background(), "my-app")
	if err != nil || len(deployments) != 1 || deployments[0].Status != app.DeploymentStatusFailed {
		t.Fatalf("deployments = %#v, %v; cancelled context prevented terminal persistence", deployments, err)
	}
}

func TestExecutorRedactsPartialConfigResolutionFailures(t *testing.T) {
	for _, stage := range []string{"resolve", "redaction"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			sqlite, checkout := newPersistenceFixture(t)
			secret := "partial-config-secret"
			cause := errors.New("config failed with " + secret)
			resolver := persistencePartialResolver{values: map[string]string{"TOKEN": secret}}
			if stage == "resolve" {
				resolver.resolveErr = cause
			} else {
				resolver.redactErr = cause
			}
			executor := newTestExecutor(t, Config{Store: sqlite, Checkout: checkout, Runner: &compose.FakeRunner{}, ConfigResolver: resolver})

			err := executeAttempt(t, ctx, sqlite, executor, "my-app", "current", "dep_config")
			if !errors.Is(err, cause) || strings.Contains(err.Error(), secret) {
				t.Fatalf("Execute error = %v", err)
			}
			deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
			if err != nil || len(deployments) != 1 || strings.Contains(deployments[0].FailureDetail, secret) || !strings.Contains(deployments[0].FailureDetail, "<redacted>") {
				t.Fatalf("deployments = %#v, %v", deployments, err)
			}
			events, err := sqlite.ListEventsByApp(ctx, "my-app")
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if strings.Contains(event.Message, secret) {
					t.Fatalf("event leaked secret: %#v", event)
				}
			}
		})
	}
}

func TestExecutorFailInterruptedFinalizesOnlyClaimedAttempts(t *testing.T) {
	for _, trigger := range []app.DeploymentTrigger{app.DeploymentTriggerPush, app.DeploymentTriggerRedeploy} {
		for _, claimed := range []bool{false, true} {
			name := string(trigger) + "/pending"
			if claimed {
				name = string(trigger) + "/deploying"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				sqlite, _ := newPersistenceFixture(t)
				now := time.Now().UTC()
				release := app.Release{ID: app.ReleaseID("my-app", "current"), AppID: "my-app", CommitSHA: "current", Status: app.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now}
				if err := sqlite.CreateRelease(ctx, release); err != nil {
					t.Fatal(err)
				}
				attempt := app.Deployment{ID: "dep_interrupted", AppID: "my-app", ReleaseID: release.ID, CommitSHA: "current", Trigger: trigger, Status: app.DeploymentStatusPending, StartedAt: now}
				if trigger == app.DeploymentTriggerRedeploy {
					if err := sqlite.QueueRedeployment(ctx, attempt); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := sqlite.QueueDeployment(ctx, attempt, ""); err != nil {
						t.Fatal(err)
					}
					if err := sqlite.RecordDeploymentQueued(ctx,
						app.Event{ID: eventID(attempt.ID, "git_ref_accepted"), AppID: attempt.AppID, Type: "git.ref_accepted", CreatedAt: now},
						app.Event{ID: eventID(attempt.ID, "queued"), AppID: attempt.AppID, Type: "deploy.queued", CreatedAt: now},
					); err != nil {
						t.Fatal(err)
					}
				}
				if claimed {
					var found bool
					var err error
					attempt, found, err = sqlite.ClaimNextPendingDeployment(ctx)
					if err != nil || !found {
						t.Fatalf("ClaimNextPendingDeployment = %#v, %v, %v", attempt, found, err)
					}
				}
				executor := newTestExecutor(t, Config{Store: sqlite})
				err := executor.FailInterrupted(ctx, attempt)
				if claimed && err != nil || !claimed && err == nil {
					t.Fatalf("FailInterrupted = %v, claimed = %v", err, claimed)
				}
				attempts, err := sqlite.ListDeploymentsByApp(ctx, attempt.AppID)
				if err != nil || len(attempts) != 1 {
					t.Fatalf("attempts = %#v, %v", attempts, err)
				}
				log, err := sqlite.DeploymentLog(ctx, attempt.AppID, attempt.ID)
				if err != nil {
					t.Fatal(err)
				}
				events, err := sqlite.ListEventsByApp(ctx, attempt.AppID)
				if err != nil {
					t.Fatal(err)
				}
				failedEvent := false
				for _, event := range events {
					failedEvent = failedEvent || event.Type == "deploy.failed"
				}
				if claimed {
					if attempts[0].Status != app.DeploymentStatusFailed || attempts[0].FailureStage != "daemon restart" || attempts[0].FinishedAt.IsZero() || !strings.Contains(attempts[0].RetryGuidance, "apps redeploy my-app") || !strings.Contains(log.Content, "deploy: failed stage=daemon restart") || !failedEvent {
						t.Fatalf("interrupted attempt = %#v, log = %#v, events = %#v", attempts[0], log, events)
					}
				} else if attempts[0] != attempt || strings.Contains(log.Content, "deploy: failed") || failedEvent {
					t.Fatalf("pending attempt changed = %#v, log = %#v, events = %#v", attempts[0], log, events)
				}
				storedRelease, err := sqlite.GetRelease(ctx, release.ID)
				if err != nil || storedRelease != release {
					t.Fatalf("historical success changed = %#v, %v", storedRelease, err)
				}
			})
		}
	}
}

func TestRedeployWaitsForRoutingAndTerminalPersistence(t *testing.T) {
	for _, stage := range []string{"routing", "app status", "failure release"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			sqlite, checkout := newPersistenceFixture(t)
			if err := sqlite.SetServerConfig(ctx, store.ServerConfig{BaseDomain: "example.com", GitHost: "sshdock.example.com"}); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			var executionStore executionStore = sqlite
			var routes routeSyncer = &fakeExecutorRouteSyncer{}
			var deployErr error
			if stage == "routing" {
				routes = &blockingTerminalRouter{entered: entered, release: release}
			} else if stage == "app status" {
				executionStore = &blockingSuccessStore{SQLiteStore: sqlite, entered: entered, release: release}
			} else {
				deployErr = errors.New("compose failed")
				executionStore = &blockingFailureStore{SQLiteStore: sqlite, entered: entered, release: release}
			}
			executor := newTestExecutor(t, Config{Store: executionStore, Checkout: checkout, Routing: testRouting(t, sqlite, routes, nil), Runner: &compose.FakeRunner{DeployErr: deployErr, DeployResult: compose.DeployResult{RouteFound: true, RouteTarget: compose.RouteTarget{ServiceName: "web", Port: 3100}}}})
			polled, pollAgain := make(chan struct{}, 1), make(chan struct{}, 1)
			producer := NewRedeployer(RedeployConfig{Store: sqlite, CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) { return "current", nil }), Poll: func(ctx context.Context) error {
				polled <- struct{}{}
				select {
				case <-pollAgain:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}})
			producerDone := make(chan error, 1)
			go func() { producerDone <- producer.RedeployCurrentMain(ctx, "my-app", "dep_wait") }()
			awaitPoll := func() {
				t.Helper()
				select {
				case <-polled:
				case err := <-producerDone:
					t.Fatalf("redeploy returned before %s finished: %v", stage, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			awaitPoll()
			attempt, found, err := sqlite.ClaimNextPendingDeployment(ctx)
			if err != nil || !found {
				t.Fatalf("ClaimNextPendingDeployment = %#v, %v, %v", attempt, found, err)
			}
			executorDone := make(chan error, 1)
			go func() { executorDone <- executor.Execute(ctx, attempt) }()
			select {
			case <-entered:
			case err := <-executorDone:
				t.Fatalf("executor finished before reaching %s: %v", stage, err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			pollAgain <- struct{}{}
			awaitPoll()
			close(release)
			select {
			case err := <-executorDone:
				if !errors.Is(err, deployErr) {
					t.Fatalf("Execute = %v, want %v", err, deployErr)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			pollAgain <- struct{}{}
			select {
			case err := <-producerDone:
				if deployErr == nil && err != nil || deployErr != nil && (err == nil || !strings.Contains(err.Error(), deployErr.Error())) {
					t.Fatalf("RedeployCurrentMain = %v, want terminal execution outcome %v", err, deployErr)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func newPersistenceFixture(t *testing.T) (*store.SQLiteStore, worktreeCheckout) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	sqlite, err := store.OpenSQLite(ctx, filepath.Join(dir, "sshdock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	worktree := filepath.Join(dir, "worktree")
	now := time.Now().UTC()
	if err := sqlite.CreateApp(ctx, app.App{ID: "my-app", Name: "my-app", RepoPath: filepath.Join(dir, "repo.git"), WorktreePath: worktree, Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return sqlite, persistenceCheckout{}
}

type persistenceCheckout struct{}

func (persistenceCheckout) Checkout(_ context.Context, _ string, worktree string, _ string) error {
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(worktree, "compose.yml"), []byte("services:\n  web:\n    image: nginx:alpine\n"), 0o644)
}

type persistenceFailureStore struct {
	*store.SQLiteStore
	deploymentErr error
	releaseErr    error
	successLogErr error
}

type blockingTerminalRouter struct {
	entered chan struct{}
	release chan struct{}
}

func (r *blockingTerminalRouter) SyncRoutes(ctx context.Context, _ []router.Route) error {
	close(r.entered)
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type blockingSuccessStore struct {
	*store.SQLiteStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockingSuccessStore) UpdateAppStatus(ctx context.Context, appID string, status app.AppStatus, now time.Time) error {
	if status == app.AppStatusHealthy {
		close(s.entered)
		<-s.release
	}
	return s.SQLiteStore.UpdateAppStatus(ctx, appID, status, now)
}

type blockingFailureStore struct {
	*store.SQLiteStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockingFailureStore) MarkReleaseFailedUnlessSucceeded(ctx context.Context, releaseID string, now time.Time) error {
	close(s.entered)
	<-s.release
	return s.SQLiteStore.MarkReleaseFailedUnlessSucceeded(ctx, releaseID, now)
}

type persistenceCancellingRunner struct {
	*compose.FakeRunner
	cancel context.CancelFunc
}

func (r *persistenceCancellingRunner) DeployWithOutput(ctx context.Context, _ compose.DeployRequest, _ io.Writer, _ io.Writer) (compose.DeployResult, error) {
	r.cancel()
	return compose.DeployResult{}, ctx.Err()
}

type persistencePartialResolver struct {
	values     map[string]string
	resolveErr error
	redactErr  error
}

func (r persistencePartialResolver) ResolveEnv(context.Context, string) (map[string]string, error) {
	return r.values, r.resolveErr
}

func (r persistencePartialResolver) RedactionValues(context.Context, string) (map[string]string, error) {
	return nil, r.redactErr
}

func (s *persistenceFailureStore) UpdateDeploymentFailure(ctx context.Context, deployment app.Deployment) error {
	if s.deploymentErr != nil {
		return s.deploymentErr
	}
	return s.SQLiteStore.UpdateDeploymentFailure(ctx, deployment)
}

func (s *persistenceFailureStore) MarkReleaseFailedUnlessSucceeded(ctx context.Context, id string, now time.Time) error {
	if s.releaseErr != nil {
		return s.releaseErr
	}
	return s.SQLiteStore.MarkReleaseFailedUnlessSucceeded(ctx, id, now)
}

func (s *persistenceFailureStore) AppendDeploymentLog(ctx context.Context, appID string, deploymentID string, output string, now time.Time) error {
	if output == "deploy: succeeded\n" && s.successLogErr != nil {
		return s.successLogErr
	}
	return s.SQLiteStore.AppendDeploymentLog(ctx, appID, deploymentID, output, now)
}

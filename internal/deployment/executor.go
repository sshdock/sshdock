package deployment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/deployfailure"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

type executionStore interface {
	GetApp(context.Context, string) (app.App, error)
	CreateRelease(context.Context, app.Release) error
	GetReleaseByAppCommit(context.Context, string, string) (app.Release, error)
	UpdateAppStatus(context.Context, string, app.AppStatus, time.Time) error
	UpdateReleaseStatus(context.Context, string, app.ReleaseStatus, time.Time) error
	MarkReleaseDeployingUnlessSucceeded(context.Context, string, time.Time) error
	MarkReleaseFailedUnlessSucceeded(context.Context, string, time.Time) error
	UpdateDeploymentStatus(context.Context, string, app.DeploymentStatus, time.Time, string) error
	UpdateDeploymentFailure(context.Context, app.Deployment) error
	AppendDeploymentLog(context.Context, string, string, string, time.Time) error
	CreateEvent(context.Context, app.Event) error
}

type initialRouter interface {
	EnsureInitial(context.Context, router.InitialRouteRequest) (string, error)
}

type configResolver interface {
	ResolveEnv(context.Context, string) (map[string]string, error)
}

type configRedactor interface {
	RedactionValues(context.Context, string) (map[string]string, error)
}

type worktreeCheckout interface {
	Checkout(context.Context, string, string, string) error
}

type Config struct {
	Store          executionStore
	Runner         compose.Runner
	Routing        initialRouter
	ConfigResolver configResolver
	Checkout       worktreeCheckout
	Now            func() time.Time
	Output         io.Writer
}

type Executor struct {
	store          executionStore
	runner         compose.Runner
	routing        initialRouter
	configResolver configResolver
	checkout       worktreeCheckout
	now            func() time.Time
	output         io.Writer
}

func NewExecutor(config Config) *Executor {
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Output == nil {
		config.Output = io.Discard
	}
	return &Executor{store: config.Store, runner: config.Runner, routing: config.Routing,
		configResolver: config.ConfigResolver, checkout: config.Checkout,
		now: config.Now, output: config.Output}
}

type attempt struct {
	deployment    app.Deployment
	release       app.Release
	releaseStored bool
}

// Execute owns a previously admitted attempt until its terminal state is recorded.
func (e *Executor) Execute(ctx context.Context, deployment app.Deployment) (executionErr error) {
	if err := e.validate(); err != nil {
		return err
	}
	if deployment.Status != app.DeploymentStatusDeploying {
		return fmt.Errorf("deployment %q is %q, want %q", deployment.ID, deployment.Status, app.DeploymentStatusDeploying)
	}
	if deployment.ID == "" || deployment.AppID == "" || deployment.ReleaseID == "" || deployment.CommitSHA == "" {
		return fmt.Errorf("deployment requires an ID, app, release, and commit")
	}
	now := e.now()
	current := attempt{deployment: deployment, release: app.Release{ID: deployment.ReleaseID, AppID: deployment.AppID,
		CommitSHA: deployment.CommitSHA, Status: app.ReleaseStatusPending, CreatedAt: now, UpdatedAt: now}}
	var redactionValues map[string]string
	defer func() { executionErr = compose.RedactError(executionErr, redactionValues) }()
	retryGuidance := "sudo sshdock apps redeploy " + deployment.AppID
	fail := func(stage string, cause error) error {
		cause = compose.RedactError(cause, redactionValues)
		return errors.Join(cause, e.recordFailedAttempt(ctx, current, stage, cause, retryGuidance))
	}
	model, err := e.store.GetApp(ctx, deployment.AppID)
	if err != nil {
		return fail("load app", err)
	}
	release, err := e.store.GetReleaseByAppCommit(ctx, deployment.AppID, deployment.CommitSHA)
	if err == nil {
		current.release, current.releaseStored = release, true
		if release.ID != deployment.ReleaseID {
			return fail("record release", fmt.Errorf("deployment release %q does not match app commit release %q", deployment.ReleaseID, release.ID))
		}
		if err := e.store.MarkReleaseDeployingUnlessSucceeded(ctx, release.ID, now); err != nil {
			return fail("record release", err)
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return fail("load release", err)
	}
	if err := e.store.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "deploy: execution started\n", now); err != nil {
		return fail("deployment log", err)
	}
	if err := e.store.CreateEvent(ctx, app.Event{ID: eventID(deployment.ID, "started"), AppID: deployment.AppID,
		Type: "deploy.started", Message: "Deploy started for release " + deployment.ReleaseID, CreatedAt: now}); err != nil {
		return fail("record start", err)
	}
	if err := e.checkout.Checkout(ctx, model.RepoPath, model.WorktreePath, deployment.CommitSHA); err != nil {
		failure := deployfailure.New("checkout", err, "deployment "+deployment.ID+" marked failed before Compose started; containers and routes were not changed", "inspect the Git repository and selected commit", retryGuidance)
		return fail("checkout", failure)
	}
	composePath, err := compose.DetectFile(model.WorktreePath)
	if err != nil {
		retryGuidance = "commit a supported Compose file and push it to remote main"
		failure := deployfailure.New("detect compose", err, "deployment "+deployment.ID+" marked failed before Compose started; containers and routes were not changed", "add compose.yml or docker-compose.yml to the selected commit", retryGuidance)
		return fail("detect compose", failure)
	}
	if !current.releaseStored {
		current.release.ComposePath = composePath
		current.release.Status = app.ReleaseStatusDeploying
		if err := e.store.CreateRelease(ctx, current.release); err != nil {
			existing, lookupErr := e.store.GetReleaseByAppCommit(ctx, deployment.AppID, deployment.CommitSHA)
			if lookupErr != nil {
				return fail("record release", errors.Join(err, lookupErr))
			}
			if existing.ID != deployment.ReleaseID {
				return fail("record release", errors.Join(err, fmt.Errorf("concurrent release %q does not match deployment release %q", existing.ID, deployment.ReleaseID)))
			}
			current.release = existing
		}
		current.releaseStored = true
	}
	var env map[string]string
	if e.configResolver != nil {
		env, err = e.configResolver.ResolveEnv(ctx, deployment.AppID)
		redactionValues = env
		if err != nil {
			failure := deployfailure.New("config", err, "release "+deployment.ReleaseID+" and deployment "+deployment.ID+" marked failed before Compose started; containers and routes were not changed", "set the missing config value(s) with the command(s) in detail", retryGuidance)
			return fail("config", failure)
		}
	}
	redactionValues = env
	if redactor, ok := e.configResolver.(configRedactor); ok {
		values, err := redactor.RedactionValues(ctx, deployment.AppID)
		if err != nil {
			return fail("config", compose.RedactError(err, env))
		}
		redactionValues = values
	}
	env = compose.WithGitSHA(env, deployment.CommitSHA)
	output := e.newDeploymentLogWriter(ctx, deployment, redactionValues)
	if _, err := compose.ValidateFileWithEnv(composePath, env); err != nil {
		err = compose.RedactError(err, redactionValues)
		_, writeErr := fmt.Fprintln(output, "stage: validate compose\n"+err.Error())
		err = errors.Join(err, writeErr, output.Flush())
		stage := string(compose.DeployStageValidateCompose)
		retryGuidance = "commit a Compose fix and push it to remote main"
		return fail(stage, deployfailure.New(stage, err, changedState(deployment, stage), deployfailure.FixForStage(stage), retryGuidance))
	}
	request := compose.DeployRequest{AppName: deployment.AppID, ProjectDir: model.WorktreePath, ComposePath: composePath,
		ReleaseID: deployment.ReleaseID, CommitSHA: deployment.CommitSHA, Env: env}
	var result compose.DeployResult
	if runner, ok := e.runner.(compose.DeploymentOutputRunner); ok {
		result, err = runner.DeployWithOutput(ctx, request, output, output)
	} else {
		result, err = e.runner.Deploy(ctx, request)
	}
	err = errors.Join(err, output.Flush())
	warningErr := compose.RedactError(e.recordDeployWarnings(ctx, deployment, result.Warnings, redactionValues), redactionValues)
	if err != nil {
		err = compose.RedactError(errors.Join(err, warningErr), redactionValues)
		stage := deployfailure.Stage(err)
		retryGuidance = "push a fixed commit to remote main, or run sudo sshdock apps redeploy " + deployment.AppID + " after a transient fix"
		return fail(stage, deployfailure.New(stage, err, changedState(deployment, stage), deployfailure.FixForStage(stage), retryGuidance))
	}
	finishedAt := e.now()
	terminalCtx := context.WithoutCancel(ctx)
	succeededMessage := "Deploy succeeded for release " + deployment.ReleaseID
	if warningErr != nil {
		succeededMessage += "; one or more deploy warnings could not be delivered or recorded: " + warningErr.Error()
	}
	result.RouteReason = compose.RedactValues(result.RouteReason, redactionValues)
	routeErr := e.autoRoute(terminalCtx, autoRouteRequest{AppName: deployment.AppID, Result: result, DeploymentID: deployment.ID, CreatedAt: finishedAt})
	// Record every outcome even if an independent log or event write fails.
	stateErr := errors.Join(
		e.store.UpdateReleaseStatus(terminalCtx, deployment.ReleaseID, app.ReleaseStatusSucceeded, finishedAt),
		e.store.UpdateAppStatus(terminalCtx, deployment.AppID, app.AppStatusHealthy, finishedAt),
		e.store.AppendDeploymentLog(terminalCtx, deployment.AppID, deployment.ID, "deploy: succeeded\n", finishedAt),
		e.store.CreateEvent(terminalCtx, app.Event{ID: eventID(deployment.ID, "succeeded"), AppID: deployment.AppID,
			Type: "deploy.succeeded", Message: succeededMessage, CreatedAt: finishedAt}),
	)
	outcomeErr := compose.RedactError(errors.Join(routeErr, stateErr), redactionValues)
	detail := ""
	if outcomeErr != nil {
		detail = "containers deployed, but record deployment outcome: " + outcomeErr.Error()
	}
	statusErr := e.store.UpdateDeploymentStatus(terminalCtx, deployment.ID, app.DeploymentStatusSucceeded, finishedAt, detail)
	if outcomeErr != nil {
		outcomeErr = fmt.Errorf("containers deployed, but record deployment outcome: %w", outcomeErr)
	}
	return errors.Join(outcomeErr, statusErr)
}

func (e *Executor) validate() error {
	if e.store == nil || e.runner == nil || e.checkout == nil {
		return fmt.Errorf("deployment store, Compose runner, and worktree checkout must be configured")
	}
	return nil
}

// FailInterrupted finalizes work owned by a previous daemon without replaying it.
func (e *Executor) FailInterrupted(ctx context.Context, model app.Deployment) error {
	if model.Status != app.DeploymentStatusDeploying {
		return fmt.Errorf("deployment %q is %q, want deploying for interrupted recovery", model.ID, model.Status)
	}
	current := attempt{deployment: model}
	release, err := e.store.GetReleaseByAppCommit(ctx, model.AppID, model.CommitSHA)
	if err == nil {
		current.release, current.releaseStored = release, true
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return e.recordFailedAttempt(ctx, current, "daemon restart",
		errors.New("daemon restarted while this deployment was active; it was not automatically rerun"),
		"inspect failure detail and run sudo sshdock apps redeploy "+model.AppID)
}

func (e *Executor) recordFailedAttempt(ctx context.Context, current attempt, stage string, cause error, retryGuidance string) error {
	ctx = context.WithoutCancel(ctx)
	finishedAt := e.now()
	deployment := current.deployment
	deployment.Status = app.DeploymentStatusFailed
	deployment.FinishedAt = finishedAt
	deployment.FailureStage = stage
	deployment.FailureDetail = cause.Error()
	deployment.ErrorMessage = cause.Error()
	deployment.RetryGuidance = retryGuidance
	persistenceErr := errors.Join(
		e.store.UpdateAppStatus(ctx, deployment.AppID, app.AppStatusFailed, finishedAt),
		e.store.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "deploy: failed stage="+stage+"; inspect deployment failure detail and retry guidance\n", finishedAt),
		e.store.CreateEvent(ctx, app.Event{ID: eventID(deployment.ID, "failed"), AppID: deployment.AppID,
			Type: "deploy.failed", Message: "Deploy failed for release " + deployment.ReleaseID + ": " + cause.Error(), CreatedAt: finishedAt}),
	)
	if current.releaseStored {
		persistenceErr = errors.Join(persistenceErr, e.store.MarkReleaseFailedUnlessSucceeded(ctx, current.release.ID, finishedAt))
	}
	return errors.Join(persistenceErr, e.store.UpdateDeploymentFailure(ctx, deployment))
}

func changedState(deployment app.Deployment, stage string) string {
	prefix := "release " + deployment.ReleaseID + " and deployment " + deployment.ID + " marked failed"
	switch compose.DeployStage(stage) {
	case compose.DeployStageComposeConfig, compose.DeployStageValidateCompose, compose.DeployStagePullImages, compose.DeployStageBuildServices:
		return prefix + " before containers started; routes were not changed"
	case compose.DeployStageWaitServices:
		return prefix + " while starting or waiting for services; routes were not changed; no automatic rollback was attempted"
	default:
		return prefix + "; inspect the detail before assuming container or route state"
	}
}

func (e *Executor) recordDeployWarnings(ctx context.Context, deployment app.Deployment, warnings []string, values map[string]string) error {
	var result error
	for index, warning := range warnings {
		warning = compose.RedactValues(warning, values)
		if err := e.store.CreateEvent(ctx, app.Event{ID: eventID(deployment.ID, fmt.Sprintf("warning_%d", index+1)), AppID: deployment.AppID,
			Type: "deploy.warning", Message: warning, CreatedAt: e.now()}); err != nil {
			result = errors.Join(result, fmt.Errorf("record deploy warning: %w", err))
		}
		if err := e.store.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "warning: "+warning+"\n", e.now()); err != nil {
			result = errors.Join(result, fmt.Errorf("record deploy warning output: %w", err))
		}
		if _, err := fmt.Fprintln(e.output, "warning: "+warning); err != nil {
			result = errors.Join(result, fmt.Errorf("print deploy warning: %w", err))
		}
	}
	return result
}

func eventID(deploymentID string, suffix string) string {
	return "evt_" + deploymentID + "_" + suffix
}

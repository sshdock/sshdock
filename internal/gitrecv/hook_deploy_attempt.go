package gitrecv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/deployfailure"
	"github.com/sshdock/sshdock/internal/store"
)

type pushAttempt struct {
	release       app.Release
	deployment    app.Deployment
	releaseStored bool
}

type pushFailure struct {
	attempt       pushAttempt
	stage         string
	cause         error
	retryGuidance string
}

func (h *PostReceiveHandler) handleEvent(ctx context.Context, event PushEvent, worktreePath string) error {
	attempt, err := h.beginPushAttempt(ctx, event)
	if err != nil {
		return err
	}

	retryGuidance := "push the same commit again after fixing the deploy failure"
	if err := h.checkout.Checkout(ctx, event.RepoPath, worktreePath, event.CommitSHA); err != nil {
		failure := deployfailure.New("checkout", err, "deployment "+attempt.deployment.ID+" marked failed before Compose started; containers and routes were not changed", "inspect the Git repository and pushed commit", retryGuidance)
		return h.recordFailedAttempt(ctx, pushFailure{attempt: attempt, stage: "checkout", cause: failure, retryGuidance: retryGuidance})
	}

	composePath, err := compose.DetectFile(worktreePath)
	if err != nil {
		failure := deployfailure.New("detect compose", err, "deployment "+attempt.deployment.ID+" marked failed before Compose started; containers and routes were not changed", "add compose.yml or docker-compose.yml to the pushed commit", retryGuidance)
		return h.recordFailedAttempt(ctx, pushFailure{attempt: attempt, stage: "detect compose", cause: failure, retryGuidance: retryGuidance})
	}
	attempt, err = h.ensureRelease(ctx, attempt, composePath)
	if err != nil {
		return err
	}
	releaseID := attempt.release.ID
	deploymentID := attempt.deployment.ID
	env, envErr := h.resolveDeployEnv(ctx, event.AppName, worktreePath)

	if envErr != nil {
		retryGuidance := "push the same commit again after fixing config"
		failure := deployfailure.New("config", envErr, "release "+releaseID+" and deployment "+deploymentID+" marked failed before Compose started; containers and routes were not changed", "set the missing config value(s) with the command(s) in detail", retryGuidance)
		return h.recordFailedAttempt(ctx, pushFailure{attempt: attempt, stage: "config", cause: failure, retryGuidance: retryGuidance})
	}
	if _, err := compose.ValidateFileWithEnv(composePath, env); err != nil {
		err = compose.RedactError(err, env)
		stage := string(compose.DeployStageValidateCompose)
		retryGuidance := "push the same commit again after fixing the deploy failure"
		failure := deployfailure.New(stage, err, gitPushChangedState(releaseID, deploymentID, stage), deployfailure.FixForStage(stage), retryGuidance)
		return h.recordFailedAttempt(ctx, pushFailure{attempt: attempt, stage: stage, cause: failure, retryGuidance: retryGuidance})
	}

	result, err := h.runner.Deploy(ctx, compose.DeployRequest{AppName: event.AppName, ProjectDir: worktreePath, ComposePath: composePath, ReleaseID: releaseID, CommitSHA: event.CommitSHA, Env: env})
	warningErr := h.recordDeployWarnings(ctx, event.AppName, deploymentID, result.Warnings, env)
	if err != nil {
		err = compose.RedactError(err, env)
		stage := deployfailure.Stage(err)
		retryGuidance := "push the same commit again after fixing the deploy failure"
		failure := deployfailure.New(stage, err, gitPushChangedState(releaseID, deploymentID, stage), deployfailure.FixForStage(stage), retryGuidance)
		return h.recordFailedAttempt(ctx, pushFailure{attempt: attempt, stage: stage, cause: failure, retryGuidance: retryGuidance})
	}

	finishedAt := h.now()
	if err := h.store.UpdateDeploymentStatus(ctx, deploymentID, app.DeploymentStatusSucceeded, finishedAt, ""); err != nil {
		return err
	}
	if err := h.store.UpdateReleaseStatus(ctx, releaseID, app.ReleaseStatusSucceeded, finishedAt); err != nil {
		return err
	}
	if err := h.store.UpdateAppStatus(ctx, event.AppName, app.AppStatusHealthy, finishedAt); err != nil {
		return err
	}
	succeededMessage := "Deploy succeeded for release " + releaseID
	if warningErr != nil {
		succeededMessage += "; one or more deploy warnings could not be delivered or recorded: " + warningErr.Error()
	}
	if err := h.store.CreateEvent(ctx, app.Event{ID: EventID(deploymentID, "succeeded"), AppID: event.AppName, Type: "deploy.succeeded", Message: succeededMessage, CreatedAt: finishedAt}); err != nil {
		return err
	}
	return h.autoRoute(ctx, event.AppName, result, deploymentID, finishedAt)
}

func (h *PostReceiveHandler) resolveDeployEnv(ctx context.Context, appName string, projectDir string) (map[string]string, error) {
	if h.configResolver == nil {
		return nil, nil
	}
	return h.configResolver.ResolveAppConfig(ctx, appName, projectDir)
}

func (h *PostReceiveHandler) beginPushAttempt(ctx context.Context, event PushEvent) (pushAttempt, error) {
	now := h.now()
	deploymentID, err := h.newDeploymentID()
	if err != nil {
		return pushAttempt{}, err
	}
	release, err := h.store.GetReleaseByAppCommit(ctx, event.AppName, event.CommitSHA)
	releaseStored := err == nil
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return pushAttempt{}, err
		}
		release = app.Release{ID: app.ReleaseID(event.AppName, event.CommitSHA), AppID: event.AppName, CommitSHA: event.CommitSHA, Status: app.ReleaseStatusPending, CreatedAt: now, UpdatedAt: now}
	}
	if err := h.store.UpdateAppStatus(ctx, event.AppName, app.AppStatusDeploying, now); err != nil {
		return pushAttempt{}, err
	}
	if releaseStored {
		if err := h.store.MarkReleaseDeployingUnlessGood(ctx, release.ID, now); err != nil {
			return pushAttempt{}, err
		}
	}
	deployment := app.Deployment{ID: deploymentID, AppID: event.AppName, ReleaseID: release.ID, CommitSHA: event.CommitSHA, Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusDeploying, StartedAt: now}
	if err := h.store.CreateDeployment(ctx, deployment); err != nil {
		return pushAttempt{}, err
	}
	if err := h.store.CreateEvent(ctx, app.Event{ID: EventID(deploymentID, "started"), AppID: event.AppName, Type: "deploy.started", Message: "Deploy started for release " + release.ID, CreatedAt: now}); err != nil {
		return pushAttempt{}, err
	}
	return pushAttempt{release: release, deployment: deployment, releaseStored: releaseStored}, nil
}

func (h *PostReceiveHandler) ensureRelease(ctx context.Context, attempt pushAttempt, composePath string) (pushAttempt, error) {
	if attempt.releaseStored {
		return attempt, nil
	}
	attempt.release.ComposePath = composePath
	attempt.release.Status = app.ReleaseStatusDeploying
	if err := h.store.CreateRelease(ctx, attempt.release); err != nil {
		existing, lookupErr := h.store.GetReleaseByAppCommit(ctx, attempt.release.AppID, attempt.release.CommitSHA)
		if lookupErr != nil {
			return pushAttempt{}, errors.Join(err, lookupErr)
		}
		attempt.release = existing
		attempt.releaseStored = true
		return attempt, nil
	}
	attempt.releaseStored = true
	return attempt, nil
}

func (h *PostReceiveHandler) recordFailedAttempt(ctx context.Context, failure pushFailure) error {
	finishedAt := h.now()
	deployment := failure.attempt.deployment
	deployment.FinishedAt = finishedAt
	deployment.FailureStage = failure.stage
	deployment.FailureDetail = failure.cause.Error()
	deployment.RetryGuidance = failure.retryGuidance
	if err := h.store.UpdateDeploymentFailure(ctx, deployment); err != nil {
		return errors.Join(failure.cause, err)
	}
	if err := h.markReleaseAttemptFailed(ctx, failure.attempt, finishedAt); err != nil {
		return errors.Join(failure.cause, err)
	}
	if err := h.store.UpdateAppStatus(ctx, deployment.AppID, app.AppStatusFailed, finishedAt); err != nil {
		return errors.Join(failure.cause, err)
	}
	if err := h.store.CreateEvent(ctx, app.Event{ID: EventID(deployment.ID, "failed"), AppID: deployment.AppID, Type: "deploy.failed", Message: "Deploy failed for release " + deployment.ReleaseID + ": " + failure.cause.Error(), CreatedAt: finishedAt}); err != nil {
		return errors.Join(failure.cause, err)
	}
	return failure.cause
}

func (h *PostReceiveHandler) markReleaseAttemptFailed(ctx context.Context, attempt pushAttempt, finishedAt time.Time) error {
	if !attempt.releaseStored {
		return nil
	}
	return h.store.MarkReleaseFailedUnlessGood(ctx, attempt.release.ID, finishedAt)
}

func gitPushChangedState(releaseID string, deploymentID string, stage string) string {
	switch compose.DeployStage(stage) {
	case compose.DeployStageComposeConfig, compose.DeployStageValidateCompose, compose.DeployStagePullImages, compose.DeployStageBuildServices:
		return "release " + releaseID + " and deployment " + deploymentID + " marked failed before containers started; routes were not changed"
	case compose.DeployStageWaitServices:
		return "release " + releaseID + " and deployment " + deploymentID + " marked failed while starting or waiting for services; routes were not changed; no automatic rollback was attempted"
	default:
		return "release " + releaseID + " and deployment " + deploymentID + " marked failed; inspect the detail before assuming container or route state"
	}
}

func (h *PostReceiveHandler) recordDeployWarnings(ctx context.Context, appName string, deploymentID string, warnings []string, env map[string]string) error {
	var result error
	for index, warning := range warnings {
		warning = compose.RedactValues(warning, env)
		if err := h.store.CreateEvent(ctx, app.Event{ID: EventID(deploymentID, fmt.Sprintf("warning_%d", index+1)), AppID: appName, Type: "deploy.warning", Message: warning, CreatedAt: h.now()}); err != nil {
			result = errors.Join(result, fmt.Errorf("record deploy warning: %w", err))
		}
		if _, err := fmt.Fprintln(h.output, "warning: "+warning); err != nil {
			result = errors.Join(result, fmt.Errorf("print deploy warning: %w", err))
		}
	}
	return result
}

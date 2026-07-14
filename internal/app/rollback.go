package app

import (
	"context"
	"fmt"

	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/deployfailure"
)

func (s *Service) RollbackRelease(ctx context.Context, appID string, releaseID string, deploymentID string) (Deployment, error) {
	model, err := s.store.GetApp(ctx, appID)
	if err != nil {
		return Deployment{}, err
	}
	release, err := s.store.GetRelease(ctx, releaseID)
	if err != nil {
		return Deployment{}, err
	}
	if release.AppID != appID {
		return Deployment{}, fmt.Errorf("release %q belongs to app %q, not %q", releaseID, release.AppID, appID)
	}

	deployment, err := s.startRecoveryDeployment(ctx, recoveryStart{
		deployment: Deployment{ID: deploymentID, AppID: appID, ReleaseID: release.ID, CommitSHA: release.CommitSHA, Trigger: DeploymentTriggerRollback},
		eventType:  "rollback.triggered",
		message:    "Rollback to release " + releaseID,
	})
	if err != nil {
		return Deployment{}, err
	}

	if s.deploy == nil {
		return deployment, nil
	}

	retryGuidance := "sudo sshdock apps rollback " + appID + " " + releaseID
	if err := s.checkoutRelease(ctx, model, release); err != nil {
		deployment = failedDeployment(deployment, "checkout", err.Error(), retryGuidance, s.now())
		_ = s.failRecoveryDeployment(ctx, recoveryFailure{deployment: deployment, eventType: "rollback.failed", message: "Rollback failed for release " + releaseID + ": " + err.Error()})
		return deployment, err
	}
	projectDir := projectDir(model, release)
	env, err := s.resolveDeployEnv(ctx, appID, projectDir)
	if err != nil {
		deployment = failedDeployment(deployment, "config", err.Error(), retryGuidance, s.now())
		_ = s.failRecoveryDeployment(ctx, recoveryFailure{deployment: deployment, eventType: "rollback.failed", message: "Rollback failed for release " + releaseID + ": " + err.Error()})
		return deployment, err
	}
	if _, err := s.deploy.Deploy(ctx, compose.DeployRequest{
		AppName: appID, ProjectDir: projectDir, ReleaseID: release.ID, CommitSHA: release.CommitSHA, ComposePath: release.ComposePath, Env: env,
	}); err != nil {
		err = compose.RedactError(err, env)
		stage := deployfailure.Stage(err)
		failure := deployfailure.New(stage, err, "rollback deployment "+deployment.ID+" marked failed; the previously running release may still be serving", deployfailure.FixForStage(stage), retryGuidance)
		deployment = failedDeployment(deployment, stage, failure.Error(), retryGuidance, s.now())
		_ = s.failRecoveryDeployment(ctx, recoveryFailure{deployment: deployment, eventType: "rollback.failed", message: "Rollback failed for release " + releaseID + ": " + failure.Error()})
		return deployment, failure
	}

	if err := s.MarkDeploymentSucceeded(ctx, deployment.ID); err != nil {
		return Deployment{}, err
	}
	finishedAt := s.now()
	if err := s.store.UpdateReleaseStatus(ctx, releaseID, ReleaseStatusRolledBack, finishedAt); err != nil {
		return Deployment{}, err
	}
	if err := s.store.UpdateAppStatus(ctx, appID, AppStatusHealthy, finishedAt); err != nil {
		return Deployment{}, err
	}
	if err := s.store.CreateEvent(ctx, Event{ID: eventID(deployment.ID, "rollback_succeeded"), AppID: appID, Type: "rollback.succeeded", Message: "Rollback succeeded for release " + releaseID, CreatedAt: finishedAt}); err != nil {
		return Deployment{}, err
	}

	deployment.Status = DeploymentStatusSucceeded
	deployment.FinishedAt = finishedAt
	return deployment, nil
}

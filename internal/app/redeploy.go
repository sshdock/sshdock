package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/deployfailure"
)

type redeployRequest struct {
	appID        string
	deploymentID string
	trigger      DeploymentTrigger
}

func (s *Service) RedeployLatest(ctx context.Context, appID string, deploymentID string) (Deployment, error) {
	return s.redeployLatest(ctx, redeployRequest{appID: appID, deploymentID: deploymentID, trigger: DeploymentTriggerRedeploy})
}

func (s *Service) redeployLatest(ctx context.Context, request redeployRequest) (Deployment, error) {
	model, release, err := s.latestGoodRelease(ctx, request.appID)
	if err != nil {
		return Deployment{}, err
	}

	deployment, err := s.startRecoveryDeployment(ctx, recoveryStart{
		deployment: Deployment{ID: request.deploymentID, AppID: request.appID, ReleaseID: release.ID, CommitSHA: release.CommitSHA, Trigger: request.trigger},
		eventType:  "redeploy.started",
		message:    "Redeploy started for release " + release.ID,
	})
	if err != nil {
		return Deployment{}, err
	}
	if s.deploy == nil {
		return deployment, nil
	}

	retryGuidance := "sudo sshdock apps redeploy " + request.appID
	if err := s.checkoutRelease(ctx, model, release); err != nil {
		deployment = failedDeployment(deployment, "checkout", err.Error(), retryGuidance, s.now())
		_ = s.failRecoveryDeployment(ctx, recoveryFailure{deployment: deployment, eventType: "redeploy.failed", message: "Redeploy failed for release " + release.ID + ": " + err.Error()})
		return deployment, err
	}
	projectDir := projectDir(model, release)
	env, err := s.resolveDeployEnv(ctx, request.appID, projectDir)
	if err != nil {
		deployment = failedDeployment(deployment, "config", err.Error(), retryGuidance, s.now())
		_ = s.failRecoveryDeployment(ctx, recoveryFailure{deployment: deployment, eventType: "redeploy.failed", message: "Redeploy failed for release " + release.ID + ": " + err.Error()})
		return deployment, err
	}
	if _, err := s.deploy.Deploy(ctx, compose.DeployRequest{AppName: request.appID, ProjectDir: projectDir, ReleaseID: release.ID, CommitSHA: release.CommitSHA, ComposePath: release.ComposePath, Env: env}); err != nil {
		err = compose.RedactError(err, env)
		stage := deployfailure.Stage(err)
		failure := deployfailure.New(stage, err, "redeploy deployment "+deployment.ID+" marked failed; the previously running release may still be serving", deployfailure.FixForStage(stage), retryGuidance)
		deployment = failedDeployment(deployment, stage, failure.Error(), retryGuidance, s.now())
		_ = s.failRecoveryDeployment(ctx, recoveryFailure{deployment: deployment, eventType: "redeploy.failed", message: "Redeploy failed for release " + release.ID + ": " + failure.Error()})
		return deployment, failure
	}

	finishedAt := s.now()
	if err := s.store.UpdateDeploymentStatus(ctx, deployment.ID, DeploymentStatusSucceeded, finishedAt, ""); err != nil {
		return Deployment{}, err
	}
	if err := s.store.UpdateReleaseStatus(ctx, release.ID, ReleaseStatusSucceeded, finishedAt); err != nil {
		return Deployment{}, err
	}
	if err := s.store.UpdateAppStatus(ctx, request.appID, AppStatusHealthy, finishedAt); err != nil {
		return Deployment{}, err
	}
	if err := s.store.CreateEvent(ctx, Event{ID: eventID(deployment.ID, "redeploy_succeeded"), AppID: request.appID, Type: "redeploy.succeeded", Message: "Redeploy succeeded for release " + release.ID, CreatedAt: finishedAt}); err != nil {
		return Deployment{}, err
	}

	deployment.Status = DeploymentStatusSucceeded
	deployment.FinishedAt = finishedAt
	return deployment, nil
}

func (s *Service) RecoverDeployedApps(ctx context.Context) error {
	apps, err := s.store.ListApps(ctx)
	if err != nil {
		return err
	}
	for _, model := range apps {
		if _, _, err := s.latestGoodRelease(ctx, model.ID); errors.Is(err, ErrNoSuccessfulRelease) {
			continue
		} else if err != nil {
			return err
		}
		deploymentID, err := s.newDeploymentID()
		if err != nil {
			return fmt.Errorf("create startup recovery attempt for app %q: %w", model.ID, err)
		}
		request := redeployRequest{appID: model.ID, deploymentID: deploymentID, trigger: DeploymentTriggerStartupRecovery}
		if _, err := s.redeployLatest(ctx, request); err != nil {
			return fmt.Errorf("recover app %q: %w", model.ID, err)
		}
	}
	return nil
}

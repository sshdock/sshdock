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

type redeployTarget struct {
	app           App
	release       Release
	releaseStored bool
}

func (s *Service) RedeployCurrentMain(ctx context.Context, appID string, deploymentID string) (Deployment, error) {
	return s.redeployCurrentMain(ctx, redeployRequest{appID: appID, deploymentID: deploymentID, trigger: DeploymentTriggerRedeploy})
}

func (s *Service) redeployCurrentMain(ctx context.Context, request redeployRequest) (Deployment, error) {
	target, err := s.currentMainTarget(ctx, request.appID)
	if err != nil {
		return Deployment{}, err
	}
	return s.redeploy(ctx, request, target)
}

func (s *Service) redeploy(ctx context.Context, request redeployRequest, target redeployTarget) (Deployment, error) {
	model := target.app
	release := target.release

	deployment, err := s.startRecoveryDeployment(ctx, recoveryStart{
		deployment: Deployment{ID: request.deploymentID, AppID: request.appID, ReleaseID: release.ID, CommitSHA: release.CommitSHA, Trigger: request.trigger},
		eventType:  "redeploy.started",
		message:    redeployEventMessage(request, target, "started"),
	})
	if err != nil {
		return Deployment{}, err
	}
	if s.deploy == nil {
		return deployment, nil
	}

	retryGuidance := redeployRetryGuidance(request)
	releaseCreated := false
	fail := func(stage string, cause error) (Deployment, error) {
		deployment = failedDeployment(deployment, stage, cause.Error(), retryGuidance, s.now())
		var persistenceErr error
		if err := s.failRecoveryDeployment(ctx, recoveryFailure{deployment: deployment, eventType: "redeploy.failed", message: redeployEventMessage(request, target, "failed: "+cause.Error())}); err != nil {
			persistenceErr = errors.Join(persistenceErr, fmt.Errorf("record failed redeploy: %w", err))
		}
		if releaseCreated {
			if err := s.store.UpdateReleaseStatus(ctx, release.ID, ReleaseStatusFailed, deployment.FinishedAt); err != nil {
				persistenceErr = errors.Join(persistenceErr, fmt.Errorf("mark release failed: %w", err))
			}
		}
		return deployment, errors.Join(cause, persistenceErr)
	}
	if err := s.checkoutRelease(ctx, model, release); err != nil {
		return fail("checkout", err)
	}
	projectDir := projectDir(model, release)
	if !target.releaseStored {
		composePath, err := compose.DetectFile(projectDir)
		if err != nil {
			retryGuidance = "commit a supported Compose file and push it to remote main"
			return fail("detect compose", err)
		}
		release.ComposePath = composePath
		release.Status = ReleaseStatusDeploying
		if err := s.store.CreateRelease(ctx, release); err != nil {
			return fail("record release", fmt.Errorf("record release for current main: %w", err))
		}
		target.release = release
		releaseCreated = true
	}
	env, err := s.resolveDeployEnv(ctx, request.appID, projectDir)
	if err != nil {
		return fail("config", err)
	}
	redactionValues, err := s.resolveRedactionValues(ctx, request.appID, env)
	if err != nil {
		return fail("config", err)
	}
	if _, err := s.deploy.Deploy(ctx, compose.DeployRequest{AppName: request.appID, ProjectDir: projectDir, ReleaseID: release.ID, CommitSHA: release.CommitSHA, ComposePath: release.ComposePath, Env: env}); err != nil {
		err = compose.RedactError(err, redactionValues)
		stage := deployfailure.Stage(err)
		failure := deployfailure.New(stage, err, "redeploy deployment "+deployment.ID+" marked failed; the previously running release may still be serving", deployfailure.FixForStage(stage), retryGuidance)
		return fail(stage, failure)
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
	if err := s.store.CreateEvent(ctx, Event{ID: eventID(deployment.ID, "redeploy_succeeded"), AppID: request.appID, Type: "redeploy.succeeded", Message: redeployEventMessage(request, target, "succeeded"), CreatedAt: finishedAt}); err != nil {
		return Deployment{}, err
	}

	deployment.Status = DeploymentStatusSucceeded
	deployment.FinishedAt = finishedAt
	return deployment, nil
}

func (s *Service) currentMainTarget(ctx context.Context, appID string) (redeployTarget, error) {
	model, err := s.store.GetApp(ctx, appID)
	if err != nil {
		return redeployTarget{}, err
	}
	if s.currentMain == nil {
		return redeployTarget{}, fmt.Errorf("resolve remote main for app %q: Git resolver is unavailable; run sudo sshdock diagnostics", appID)
	}
	commitSHA, err := s.currentMain.ResolveCurrentMain(ctx, model.RepoPath)
	if err != nil {
		return redeployTarget{}, fmt.Errorf("resolve remote main for app %q: %w", appID, err)
	}
	releases, err := s.store.ListReleasesByApp(ctx, appID)
	if err != nil {
		return redeployTarget{}, err
	}
	for _, release := range releases {
		if release.CommitSHA == commitSHA {
			return redeployTarget{app: model, release: release, releaseStored: true}, nil
		}
	}
	now := s.now()
	release := Release{ID: ReleaseID(appID, commitSHA), AppID: appID, CommitSHA: commitSHA, Status: ReleaseStatusPending, CreatedAt: now, UpdatedAt: now}
	return redeployTarget{app: model, release: release}, nil
}

func redeployEventMessage(request redeployRequest, target redeployTarget, state string) string {
	if request.trigger == DeploymentTriggerStartupRecovery {
		return "Startup recovery for release " + target.release.ID + " " + state
	}
	return "Redeploy current main " + target.release.CommitSHA + " " + state
}

func redeployRetryGuidance(request redeployRequest) string {
	if request.trigger == DeploymentTriggerStartupRecovery {
		return "fix the reported cause, then restart sshdockd to retry startup recovery of the latest good release"
	}
	return "sudo sshdock apps redeploy " + request.appID
}

func (s *Service) RecoverDeployedApps(ctx context.Context) error {
	apps, err := s.store.ListApps(ctx)
	if err != nil {
		return err
	}
	for _, model := range apps {
		model, release, err := s.latestGoodRelease(ctx, model.ID)
		if errors.Is(err, ErrNoSuccessfulRelease) {
			continue
		} else if err != nil {
			return err
		}
		deploymentID, err := s.newDeploymentID()
		if err != nil {
			return fmt.Errorf("create startup recovery attempt for app %q: %w", model.ID, err)
		}
		request := redeployRequest{appID: model.ID, deploymentID: deploymentID, trigger: DeploymentTriggerStartupRecovery}
		if _, err := s.redeploy(ctx, request, redeployTarget{app: model, release: release, releaseStored: true}); err != nil {
			return fmt.Errorf("recover app %q: %w", model.ID, err)
		}
	}
	return nil
}

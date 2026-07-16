package app

import (
	"context"
	"time"
)

type recoveryStart struct {
	deployment Deployment
	eventType  string
	message    string
}

type recoveryFailure struct {
	deployment Deployment
	eventType  string
	message    string
}

func (s *Service) checkoutCurrentMain(ctx context.Context, model App, commitSHA string) error {
	if s.checkout == nil {
		return nil
	}
	projectDir, err := currentProjectDir(model)
	if err != nil {
		return err
	}
	return s.checkout.Checkout(ctx, model.RepoPath, projectDir, commitSHA)
}

func (s *Service) resolveDeployEnv(ctx context.Context, appID string, projectDir string) (map[string]string, error) {
	if s.configResolver == nil {
		return nil, nil
	}
	return s.configResolver.ResolveAppConfig(ctx, appID)
}

func (s *Service) resolveRedactionValues(ctx context.Context, appID string, env map[string]string) (map[string]string, error) {
	redactor, ok := s.configResolver.(configRedactor)
	if !ok {
		return env, nil
	}
	return redactor.RedactionValues(ctx, appID)
}

func (s *Service) startRecoveryDeployment(ctx context.Context, start recoveryStart) (Deployment, error) {
	now := s.now()
	start.deployment.StartedAt = now
	deployment, err := s.StartDeployment(ctx, start.deployment)
	if err != nil {
		return Deployment{}, err
	}
	if err := s.store.UpdateAppStatus(ctx, deployment.AppID, AppStatusDeploying, now); err != nil {
		return Deployment{}, err
	}
	if err := s.store.CreateEvent(ctx, Event{ID: eventID(deployment.ID, start.eventType), AppID: deployment.AppID, Type: start.eventType, Message: start.message, CreatedAt: now}); err != nil {
		return Deployment{}, err
	}
	return deployment, nil
}

func (s *Service) failRecoveryDeployment(ctx context.Context, failure recoveryFailure) error {
	deployment := failure.deployment
	if err := s.store.UpdateDeploymentFailure(ctx, deployment); err != nil {
		return err
	}
	if err := s.store.UpdateAppStatus(ctx, deployment.AppID, AppStatusFailed, deployment.FinishedAt); err != nil {
		return err
	}
	return s.store.CreateEvent(ctx, Event{ID: eventID(deployment.ID, failure.eventType), AppID: deployment.AppID, Type: failure.eventType, Message: failure.message, CreatedAt: deployment.FinishedAt})
}

func failedDeployment(model Deployment, stage string, detail string, retryGuidance string, finishedAt time.Time) Deployment {
	model.Status = DeploymentStatusFailed
	model.FinishedAt = finishedAt
	model.FailureStage = stage
	model.FailureDetail = detail
	model.RetryGuidance = retryGuidance
	model.ErrorMessage = detail
	return model
}

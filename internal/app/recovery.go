package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

var ErrNoSuccessfulRelease = errors.New("no successful release")

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

func (s *Service) latestGoodRelease(ctx context.Context, appID string) (App, Release, error) {
	model, err := s.store.GetApp(ctx, appID)
	if err != nil {
		return App{}, Release{}, err
	}
	releases, err := s.store.ListReleasesByApp(ctx, appID)
	if err != nil {
		return App{}, Release{}, err
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].CreatedAt.Equal(releases[j].CreatedAt) {
			return releases[i].ID < releases[j].ID
		}
		return releases[i].CreatedAt.Before(releases[j].CreatedAt)
	})
	for i := len(releases) - 1; i >= 0; i-- {
		if releases[i].Status == ReleaseStatusSucceeded || releases[i].Status == ReleaseStatusRolledBack {
			return model, releases[i], nil
		}
	}
	return App{}, Release{}, fmt.Errorf("%w for app %q", ErrNoSuccessfulRelease, appID)
}

func (s *Service) checkoutRelease(ctx context.Context, model App, release Release) error {
	if s.checkout == nil {
		return nil
	}
	return s.checkout.Checkout(ctx, model.RepoPath, projectDir(model, release), release.CommitSHA)
}

func (s *Service) resolveDeployEnv(ctx context.Context, appID string, projectDir string) (map[string]string, error) {
	if s.configResolver == nil {
		return nil, nil
	}
	return s.configResolver.ResolveAppConfig(ctx, appID, projectDir)
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

func projectDir(model App, release Release) string {
	if model.WorktreePath != "" {
		return model.WorktreePath
	}
	return filepath.Dir(release.ComposePath)
}

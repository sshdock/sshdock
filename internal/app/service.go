package app

import (
	"context"
	"fmt"
	"time"

	"github.com/iketiunn/rumbase/internal/compose"
)

type serviceStore interface {
	CreateApp(ctx context.Context, model App) error
	GetApp(ctx context.Context, id string) (App, error)
	ListApps(ctx context.Context) ([]App, error)
	CreateRelease(ctx context.Context, model Release) error
	GetRelease(ctx context.Context, id string) (Release, error)
	ListReleasesByApp(ctx context.Context, appID string) ([]Release, error)
	CreateDeployment(ctx context.Context, model Deployment) error
	UpdateDeploymentStatus(ctx context.Context, id string, status DeploymentStatus, finishedAt time.Time, errorMessage string) error
	AttachDomain(ctx context.Context, model Domain) error
	ListDomainsByApp(ctx context.Context, appID string) ([]Domain, error)
	CreateEvent(ctx context.Context, model Event) error
	ListEventsByApp(ctx context.Context, appID string) ([]Event, error)
}

type Service struct {
	store  serviceStore
	now    func() time.Time
	logs   logsRunner
	deploy deployRunner
}

type ServiceOption func(*Service)

type logsRunner interface {
	Logs(ctx context.Context, request compose.LogsRequest) (string, error)
}

type deployRunner interface {
	Deploy(ctx context.Context, request compose.DeployRequest) error
}

func NewService(store serviceStore, options ...ServiceOption) *Service {
	service := &Service{
		store: store,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}

	for _, option := range options {
		option(service)
	}

	return service
}

func WithClock(clock func() time.Time) ServiceOption {
	return func(service *Service) {
		service.now = clock
	}
}

func WithLogRunner(runner logsRunner) ServiceOption {
	return func(service *Service) {
		service.logs = runner
	}
}

func WithDeployRunner(runner deployRunner) ServiceOption {
	return func(service *Service) {
		service.deploy = runner
	}
}

func (s *Service) CreateApp(ctx context.Context, model App) (App, error) {
	now := s.now()
	if model.Status == "" {
		model.Status = AppStatusCreated
	}
	if model.CreatedAt.IsZero() {
		model.CreatedAt = now
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = now
	}

	if err := s.store.CreateApp(ctx, model); err != nil {
		return App{}, err
	}

	return model, nil
}

func (s *Service) ListApps(ctx context.Context) ([]App, error) {
	return s.store.ListApps(ctx)
}

func (s *Service) CreateRelease(ctx context.Context, model Release) (Release, error) {
	now := s.now()
	if model.Status == "" {
		model.Status = ReleaseStatusPending
	}
	if model.CreatedAt.IsZero() {
		model.CreatedAt = now
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = now
	}

	if err := s.store.CreateRelease(ctx, model); err != nil {
		return Release{}, err
	}

	return model, nil
}

func (s *Service) StartDeployment(ctx context.Context, model Deployment) (Deployment, error) {
	if model.Status == "" {
		model.Status = DeploymentStatusDeploying
	}
	if model.StartedAt.IsZero() {
		model.StartedAt = s.now()
	}

	if err := s.store.CreateDeployment(ctx, model); err != nil {
		return Deployment{}, err
	}

	return model, nil
}

func (s *Service) MarkDeploymentSucceeded(ctx context.Context, deploymentID string) error {
	return s.store.UpdateDeploymentStatus(ctx, deploymentID, DeploymentStatusSucceeded, s.now(), "")
}

func (s *Service) MarkDeploymentFailed(ctx context.Context, deploymentID string, errorMessage string) error {
	return s.store.UpdateDeploymentStatus(ctx, deploymentID, DeploymentStatusFailed, s.now(), errorMessage)
}

func (s *Service) AttachDomain(ctx context.Context, model Domain) (Domain, error) {
	now := s.now()
	if model.CreatedAt.IsZero() {
		model.CreatedAt = now
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = now
	}

	if err := s.store.AttachDomain(ctx, model); err != nil {
		return Domain{}, err
	}

	return model, nil
}

func (s *Service) RollbackRelease(ctx context.Context, appID string, releaseID string, deploymentID string) (Deployment, error) {
	release, err := s.store.GetRelease(ctx, releaseID)
	if err != nil {
		return Deployment{}, err
	}
	if release.AppID != appID {
		return Deployment{}, fmt.Errorf("release %q belongs to app %q, not %q", releaseID, release.AppID, appID)
	}

	deployment, err := s.StartDeployment(ctx, Deployment{
		ID:        deploymentID,
		AppID:     appID,
		ReleaseID: releaseID,
	})
	if err != nil {
		return Deployment{}, err
	}

	if err := s.store.CreateEvent(ctx, Event{
		ID:        "evt_" + deploymentID + "_rollback",
		AppID:     appID,
		Type:      "rollback.triggered",
		Message:   "Rollback to release " + releaseID,
		CreatedAt: s.now(),
	}); err != nil {
		return Deployment{}, err
	}

	if s.deploy == nil {
		return deployment, nil
	}

	if err := s.deploy.Deploy(ctx, compose.DeployRequest{
		AppName:     appID,
		ReleaseID:   release.ID,
		CommitSHA:   release.CommitSHA,
		ComposePath: release.ComposePath,
	}); err != nil {
		_ = s.MarkDeploymentFailed(ctx, deployment.ID, err.Error())
		deployment.Status = DeploymentStatusFailed
		deployment.FinishedAt = s.now()
		deployment.ErrorMessage = err.Error()
		return deployment, err
	}

	if err := s.MarkDeploymentSucceeded(ctx, deployment.ID); err != nil {
		return Deployment{}, err
	}

	deployment.Status = DeploymentStatusSucceeded
	deployment.FinishedAt = s.now()
	return deployment, nil
}

func (s *Service) Logs(ctx context.Context, appName string, serviceName string, lines int) (string, error) {
	if s.logs == nil {
		return "", fmt.Errorf("logs runner is not configured")
	}

	return s.logs.Logs(ctx, compose.LogsRequest{
		AppName:     appName,
		ServiceName: serviceName,
		Lines:       lines,
	})
}

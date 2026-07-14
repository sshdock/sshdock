package app

import (
	"context"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

type serviceStore interface {
	CreateApp(ctx context.Context, model App) error
	GetApp(ctx context.Context, id string) (App, error)
	ListApps(ctx context.Context) ([]App, error)
	UpdateAppStatus(ctx context.Context, id string, status AppStatus, updatedAt time.Time) error
	CreateRelease(ctx context.Context, model Release) error
	GetRelease(ctx context.Context, id string) (Release, error)
	ListReleasesByApp(ctx context.Context, appID string) ([]Release, error)
	UpdateReleaseStatus(ctx context.Context, id string, status ReleaseStatus, updatedAt time.Time) error
	CreateDeployment(ctx context.Context, model Deployment) error
	UpdateDeploymentStatus(ctx context.Context, id string, status DeploymentStatus, finishedAt time.Time, errorMessage string) error
	UpdateDeploymentFailure(ctx context.Context, model Deployment) error
	AttachDomain(ctx context.Context, model Domain) error
	ListDomainsByApp(ctx context.Context, appID string) ([]Domain, error)
	CreateEvent(ctx context.Context, model Event) error
	ListEventsByApp(ctx context.Context, appID string) ([]Event, error)
}

type Service struct {
	store           serviceStore
	now             func() time.Time
	logs            logsRunner
	deploy          deployRunner
	recover         recoveryRunner
	checkout        WorktreeCheckout
	currentMain     CurrentMainResolver
	configResolver  configResolver
	newDeploymentID func() (string, error)
}

type ServiceOption func(*Service)

type logsRunner interface {
	Logs(ctx context.Context, request compose.LogsRequest) (string, error)
}

type deployRunner interface {
	Deploy(ctx context.Context, request compose.DeployRequest) (compose.DeployResult, error)
}

type recoveryRunner interface {
	Deploy(ctx context.Context, request compose.DeployRequest) (compose.DeployResult, error)
	Restart(ctx context.Context, request compose.RestartRequest) error
}

type WorktreeCheckout interface {
	Checkout(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error
}

type CurrentMainResolver interface {
	ResolveCurrentMain(ctx context.Context, repoPath string) (string, error)
}

type CurrentMainResolverFunc func(ctx context.Context, repoPath string) (string, error)

func (f CurrentMainResolverFunc) ResolveCurrentMain(ctx context.Context, repoPath string) (string, error) {
	return f(ctx, repoPath)
}

type configResolver interface {
	ResolveAppConfig(ctx context.Context, appID string, projectDir string) (map[string]string, error)
}

func NewService(store serviceStore, options ...ServiceOption) *Service {
	service := &Service{
		store: store,
		now: func() time.Time {
			return time.Now().UTC()
		},
		newDeploymentID: NewDeploymentID,
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
		if recovery, ok := runner.(recoveryRunner); ok {
			service.recover = recovery
		}
	}
}

func WithRecoveryRunner(runner recoveryRunner) ServiceOption {
	return func(service *Service) {
		service.deploy = runner
		service.recover = runner
	}
}

func WithWorktreeCheckout(checkout WorktreeCheckout) ServiceOption {
	return func(service *Service) {
		service.checkout = checkout
	}
}

func WithCurrentMainResolver(resolver CurrentMainResolver) ServiceOption {
	return func(service *Service) {
		service.currentMain = resolver
	}
}

func WithConfigResolver(resolver configResolver) ServiceOption {
	return func(service *Service) {
		service.configResolver = resolver
	}
}

func WithDeploymentIDGenerator(generator func() (string, error)) ServiceOption {
	return func(service *Service) {
		service.newDeploymentID = generator
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

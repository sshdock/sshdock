package app

import (
	"context"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

type serviceStore interface {
	GetApp(ctx context.Context, id string) (App, error)
	CreateEvent(ctx context.Context, model Event) error
}

type Service struct {
	store           serviceStore
	now             func() time.Time
	recover         recoveryRunner
	serviceCommands serviceCommandRunner
	configResolver  configResolver
}

type ServiceOption func(*Service)

type recoveryRunner interface {
	Start(ctx context.Context, request compose.LifecycleRequest) error
	Stop(ctx context.Context, request compose.LifecycleRequest) error
	Restart(ctx context.Context, request compose.RestartRequest) error
}

type serviceCommandRunner interface {
	Exec(ctx context.Context, request compose.ServiceCommandRequest) error
	RunOneOff(ctx context.Context, request compose.ServiceCommandRequest) error
}

type CurrentMainResolver interface {
	ResolveCurrentMain(ctx context.Context, repoPath string) (string, error)
}

type CurrentMainResolverFunc func(ctx context.Context, repoPath string) (string, error)

func (f CurrentMainResolverFunc) ResolveCurrentMain(ctx context.Context, repoPath string) (string, error) {
	return f(ctx, repoPath)
}

type configResolver interface {
	ResolveEnv(ctx context.Context, appID string) (map[string]string, error)
}

type configRedactor interface {
	RedactionValues(ctx context.Context, appID string) (map[string]string, error)
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

func WithRecoveryRunner(runner recoveryRunner) ServiceOption {
	return func(service *Service) {
		service.recover = runner
		if commands, ok := runner.(serviceCommandRunner); ok {
			service.serviceCommands = commands
		}
	}
}

func WithConfigResolver(resolver configResolver) ServiceOption {
	return func(service *Service) {
		service.configResolver = resolver
	}
}

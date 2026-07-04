package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
)

var ErrNotFound = errors.New("not found")

type ServerConfig struct {
	GitHost   string
	UpdatedAt time.Time
}

type SSHKey struct {
	Name      string
	PublicKey string
	CreatedAt time.Time
}

type Store interface {
	CreateApp(ctx context.Context, model app.App) error
	GetApp(ctx context.Context, id string) (app.App, error)
	ListApps(ctx context.Context) ([]app.App, error)
	UpdateAppStatus(ctx context.Context, id string, status app.AppStatus, updatedAt time.Time) error
	CreateRelease(ctx context.Context, model app.Release) error
	GetRelease(ctx context.Context, id string) (app.Release, error)
	ListReleasesByApp(ctx context.Context, appID string) ([]app.Release, error)
	UpdateReleaseStatus(ctx context.Context, id string, status app.ReleaseStatus, updatedAt time.Time) error
	CreateDeployment(ctx context.Context, model app.Deployment) error
	ListDeploymentsByApp(ctx context.Context, appID string) ([]app.Deployment, error)
	UpdateDeploymentStatus(ctx context.Context, id string, status app.DeploymentStatus, finishedAt time.Time, errorMessage string) error
	AttachDomain(ctx context.Context, model app.Domain) error
	ListDomains(ctx context.Context) ([]app.Domain, error)
	ListDomainsByApp(ctx context.Context, appID string) ([]app.Domain, error)
	DeleteDomainByAppAndName(ctx context.Context, appID string, domainName string) (app.Domain, error)
	CreateEvent(ctx context.Context, model app.Event) error
	ListEventsByApp(ctx context.Context, appID string) ([]app.Event, error)
	SetServerConfig(ctx context.Context, config ServerConfig) error
	GetServerConfig(ctx context.Context) (ServerConfig, error)
	UpsertSSHKey(ctx context.Context, key SSHKey) error
	ListSSHKeys(ctx context.Context) ([]SSHKey, error)
	DeleteSSHKey(ctx context.Context, name string) error
	DeleteApp(ctx context.Context, appID string) error
}

func notFound(kind string, id string) error {
	return fmt.Errorf("%s %q: %w", kind, id, ErrNotFound)
}

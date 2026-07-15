package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

type ReceiveRepoSetupper interface {
	SetupBareRepo(ctx context.Context, appName string) (gitrecv.BareRepo, error)
}

type StoreBackendConfig struct {
	NodeID                     string
	AppsDir                    string
	GitHost                    string
	AuthorizedKeysPath         string
	GitReceiveCommand          string
	OperatorAuthorizedKeysPath string
	OperatorCommand            string
	RepoSetupper               ReceiveRepoSetupper
	Router                     routeSyncer
	RecoveryRunner             compose.Runner
	RecoveryCheckout           appmodel.WorktreeCheckout
	CurrentMainResolver        appmodel.CurrentMainResolver
	ConfigManager              configManager
	Now                        func() time.Time
	NewDeploymentID            func() (string, error)
}

type routeSyncer interface {
	SyncRoutes(ctx context.Context, routes []router.Route) error
}

type routeReader interface {
	Routes(ctx context.Context) ([]router.Route, error)
}

type logStreamer interface {
	StreamLogs(ctx context.Context, request compose.LogsRequest, stdout io.Writer, stderr io.Writer) error
}

type StoreBackend struct {
	store                      store.Store
	nodeID                     string
	appsDir                    string
	gitHost                    string
	authorizedKeysPath         string
	gitReceiveCommand          string
	operatorAuthorizedKeysPath string
	operatorCommand            string
	repoSetupper               ReceiveRepoSetupper
	router                     routeSyncer
	recoveryRunner             compose.Runner
	recoveryCheckout           appmodel.WorktreeCheckout
	currentMainResolver        appmodel.CurrentMainResolver
	configManager              configManager
	now                        func() time.Time
	newDeploymentID            func() (string, error)
}

func NewStoreBackend(persistentStore store.Store, cfg StoreBackendConfig) *StoreBackend {
	if cfg.NodeID == "" {
		cfg.NodeID = "local"
	}
	if cfg.GitHost == "" {
		cfg.GitHost = "server"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.NewDeploymentID == nil {
		cfg.NewDeploymentID = appmodel.NewDeploymentID
	}

	return &StoreBackend{
		store:                      persistentStore,
		nodeID:                     cfg.NodeID,
		appsDir:                    cfg.AppsDir,
		gitHost:                    cfg.GitHost,
		authorizedKeysPath:         cfg.AuthorizedKeysPath,
		gitReceiveCommand:          cfg.GitReceiveCommand,
		operatorAuthorizedKeysPath: cfg.OperatorAuthorizedKeysPath,
		operatorCommand:            cfg.OperatorCommand,
		repoSetupper:               cfg.RepoSetupper,
		router:                     cfg.Router,
		recoveryRunner:             cfg.RecoveryRunner,
		recoveryCheckout:           cfg.RecoveryCheckout,
		currentMainResolver:        cfg.CurrentMainResolver,
		configManager:              cfg.ConfigManager,
		now:                        cfg.Now,
		newDeploymentID:            cfg.NewDeploymentID,
	}
}

func (b *StoreBackend) ListApps() ([]App, error) {
	models, err := b.store.ListApps(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}

	apps := make([]App, 0, len(models))
	for _, model := range models {
		apps = append(apps, cliApp(model))
	}
	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Name < apps[j].Name
	})

	return apps, nil
}

func (b *StoreBackend) GetApp(name string) (App, error) {
	model, err := b.store.GetApp(context.Background(), name)
	if errors.Is(err, store.ErrNotFound) {
		return App{}, fmt.Errorf("app %q not found", name)
	}
	if err != nil {
		return App{}, fmt.Errorf("get app %q: %w", name, err)
	}

	return cliApp(model), nil
}

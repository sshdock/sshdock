package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appmodel "github.com/iketiunn/rumbase/internal/app"
	"github.com/iketiunn/rumbase/internal/compose"
	"github.com/iketiunn/rumbase/internal/gitrecv"
	"github.com/iketiunn/rumbase/internal/router"
	"github.com/iketiunn/rumbase/internal/sshaccess"
	"github.com/iketiunn/rumbase/internal/store"
)

type ReceiveRepoSetupper interface {
	SetupBareRepo(ctx context.Context, appName string) (gitrecv.BareRepo, error)
}

type StoreBackendConfig struct {
	NodeID                      string
	AppsDir                     string
	GitHost                     string
	AuthorizedKeysPath          string
	GitReceiveCommand           string
	DashboardAuthorizedKeysPath string
	DashboardCommand            string
	RepoSetupper                ReceiveRepoSetupper
	Router                      routeSyncer
	RecoveryRunner              compose.Runner
	Now                         func() time.Time
}

type routeSyncer interface {
	SyncRoutes(ctx context.Context, routes []router.Route) error
}

type StoreBackend struct {
	store                       store.Store
	nodeID                      string
	appsDir                     string
	gitHost                     string
	authorizedKeysPath          string
	gitReceiveCommand           string
	dashboardAuthorizedKeysPath string
	dashboardCommand            string
	repoSetupper                ReceiveRepoSetupper
	router                      routeSyncer
	recoveryRunner              compose.Runner
	now                         func() time.Time
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

	return &StoreBackend{
		store:                       persistentStore,
		nodeID:                      cfg.NodeID,
		appsDir:                     cfg.AppsDir,
		gitHost:                     cfg.GitHost,
		authorizedKeysPath:          cfg.AuthorizedKeysPath,
		gitReceiveCommand:           cfg.GitReceiveCommand,
		dashboardAuthorizedKeysPath: cfg.DashboardAuthorizedKeysPath,
		dashboardCommand:            cfg.DashboardCommand,
		repoSetupper:                cfg.RepoSetupper,
		router:                      cfg.Router,
		recoveryRunner:              cfg.RecoveryRunner,
		now:                         cfg.Now,
	}
}

func (b *StoreBackend) CreateApp(name string) (App, string, error) {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, name); err == nil {
		return App{}, "", fmt.Errorf("app %q already exists", name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return App{}, "", fmt.Errorf("check app %q: %w", name, err)
	}

	repo := gitrecv.BareRepo{
		Path:      filepath.Join(b.appsDir, name, "repo.git"),
		RemoteURL: fmt.Sprintf("git@%s:%s.git", b.currentGitHost(ctx), name),
	}
	if b.repoSetupper != nil {
		persistedGitHost, hasPersistedGitHost := b.persistedGitHost(ctx)
		var err error
		repo, err = b.repoSetupper.SetupBareRepo(ctx, name)
		if err != nil {
			return App{}, "", fmt.Errorf("set up receive repo for app %q: %w", name, err)
		}
		if repo.Path == "" {
			repo.Path = filepath.Join(b.appsDir, name, "repo.git")
		}
		if hasPersistedGitHost {
			repo.RemoteURL = fmt.Sprintf("git@%s:%s.git", persistedGitHost, name)
		}
		if repo.RemoteURL == "" {
			repo.RemoteURL = fmt.Sprintf("git@%s:%s.git", b.currentGitHost(ctx), name)
		}
	}

	now := b.now()
	model := appmodel.App{
		ID:           name,
		Name:         name,
		NodeID:       b.nodeID,
		RepoPath:     repo.Path,
		WorktreePath: filepath.Join(b.appsDir, name, "worktree"),
		Status:       appmodel.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := b.store.CreateApp(ctx, model); err != nil {
		return App{}, "", fmt.Errorf("create app %q: %w", name, err)
	}

	return cliApp(model), repo.RemoteURL, nil
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

func (b *StoreBackend) RestartApp(name string) error {
	ctx := context.Background()
	if err := b.recoveryService().RestartApp(ctx, name); err != nil {
		return fmt.Errorf("restart app %q: %w", name, err)
	}
	return nil
}

func (b *StoreBackend) RestartService(appName string, serviceName string) error {
	ctx := context.Background()
	if err := b.recoveryService().RestartService(ctx, appName, serviceName); err != nil {
		return fmt.Errorf("restart service %q/%q: %w", appName, serviceName, err)
	}
	return nil
}

func (b *StoreBackend) RedeployApp(name string) error {
	ctx := context.Background()
	deploymentID := recoveryDeploymentID("redeploy", name, "", b.now())
	if _, err := b.recoveryService().RedeployLatest(ctx, name, deploymentID); err != nil {
		return fmt.Errorf("redeploy app %q: %w", name, err)
	}
	return nil
}

func (b *StoreBackend) RollbackApp(name string, releaseID string) error {
	ctx := context.Background()
	deploymentID := recoveryDeploymentID("rollback", name, releaseID, b.now())
	if _, err := b.recoveryService().RollbackRelease(ctx, name, releaseID, deploymentID); err != nil {
		return fmt.Errorf("rollback app %q to %q: %w", name, releaseID, err)
	}
	return nil
}

func (b *StoreBackend) AttachDomain(domain Domain) error {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, domain.AppName); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("app %q not found", domain.AppName)
	} else if err != nil {
		return fmt.Errorf("get app %q: %w", domain.AppName, err)
	}

	now := b.now()
	model := appmodel.Domain{
		ID:          domainID(domain.AppName, domain.DomainName),
		AppID:       domain.AppName,
		ServiceName: domain.ServiceName,
		DomainName:  domain.DomainName,
		Port:        domain.Port,
		HTTPS:       true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := b.store.AttachDomain(ctx, model); err != nil {
		return fmt.Errorf("attach domain %q: %w", domain.DomainName, err)
	}
	if err := b.store.CreateEvent(ctx, appmodel.Event{
		ID:        eventID(model.ID, "attached"),
		AppID:     model.AppID,
		Type:      "domain.attached",
		Message:   "Attached " + model.DomainName + " to " + model.AppID + "/" + model.ServiceName,
		CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("record domain attach event: %w", err)
	}
	if b.router != nil {
		domains, err := b.store.ListDomains(ctx)
		if err != nil {
			return fmt.Errorf("list domains for route rebuild: %w", err)
		}
		if err := b.router.SyncRoutes(ctx, routesFromDomains(domains)); err != nil {
			_ = b.store.CreateEvent(ctx, appmodel.Event{
				ID:        eventID(model.ID, "router_reload_failed"),
				AppID:     model.AppID,
				Type:      "router.reload_failed",
				Message:   "Caddy reload failed for " + model.DomainName + ": " + err.Error(),
				CreatedAt: b.now(),
			})
			return fmt.Errorf("reload Caddy routes: %w", err)
		}
		if err := b.store.CreateEvent(ctx, appmodel.Event{
			ID:        eventID(model.ID, "router_reloaded"),
			AppID:     model.AppID,
			Type:      "router.reloaded",
			Message:   "Reloaded Caddy routes for " + model.DomainName,
			CreatedAt: b.now(),
		}); err != nil {
			return fmt.Errorf("record Caddy reload event: %w", err)
		}
	}

	return nil
}

func (b *StoreBackend) SetServerGitHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("server Git host is required")
	}

	if err := b.store.SetServerConfig(context.Background(), store.ServerConfig{
		GitHost:   host,
		UpdatedAt: b.now(),
	}); err != nil {
		return fmt.Errorf("set server Git host: %w", err)
	}

	return nil
}

func (b *StoreBackend) AddSSHKey(name string, publicKey string) error {
	name = strings.TrimSpace(name)
	publicKey = strings.TrimSpace(publicKey)
	if name == "" {
		return fmt.Errorf("SSH key name is required")
	}
	if err := validatePublicKey(publicKey); err != nil {
		return err
	}
	ctx := context.Background()
	key := store.SSHKey{Name: name, PublicKey: publicKey, CreatedAt: b.now()}
	if err := b.store.UpsertSSHKey(ctx, key); err != nil {
		return fmt.Errorf("store SSH key %q: %w", name, err)
	}
	keys, err := b.store.ListSSHKeys(ctx)
	if err != nil {
		return fmt.Errorf("list SSH keys: %w", err)
	}
	if b.authorizedKeysPath != "" {
		if err := sshaccess.WriteAuthorizedKeys(b.authorizedKeysPath, sshAccessKeys(keys), b.gitReceiveCommand); err != nil {
			return fmt.Errorf("write authorized_keys: %w", err)
		}
	}
	if b.dashboardAuthorizedKeysPath != "" {
		if err := sshaccess.WriteDashboardAuthorizedKeys(b.dashboardAuthorizedKeysPath, sshAccessKeys(keys), b.dashboardCommand); err != nil {
			return fmt.Errorf("write dashboard authorized_keys: %w", err)
		}
	}

	return nil
}

func cliApp(model appmodel.App) App {
	return App{
		Name:   model.Name,
		Status: string(model.Status),
		NodeID: model.NodeID,
	}
}

func (b *StoreBackend) recoveryService() *appmodel.Service {
	options := []appmodel.ServiceOption{appmodel.WithClock(b.now)}
	if b.recoveryRunner != nil {
		options = append(options, appmodel.WithRecoveryRunner(b.recoveryRunner))
	}
	return appmodel.NewService(b.store, options...)
}

func (b *StoreBackend) currentGitHost(ctx context.Context) string {
	if gitHost, ok := b.persistedGitHost(ctx); ok {
		return gitHost
	}
	return b.gitHost
}

func (b *StoreBackend) persistedGitHost(ctx context.Context) (string, bool) {
	config, err := b.store.GetServerConfig(ctx)
	if err == nil && config.GitHost != "" {
		return config.GitHost, true
	}
	return "", false
}

func sshAccessKeys(keys []store.SSHKey) []sshaccess.Key {
	result := make([]sshaccess.Key, 0, len(keys))
	for _, key := range keys {
		result = append(result, sshaccess.Key{
			Name:      key.Name,
			PublicKey: key.PublicKey,
			CreatedAt: key.CreatedAt,
		})
	}
	return result
}

func domainID(appName string, domainName string) string {
	return "dom_" + sanitizeIDPart(appName) + "_" + sanitizeIDPart(domainName)
}

func eventID(subjectID string, suffix string) string {
	return "evt_" + sanitizeIDPart(subjectID) + "_" + sanitizeIDPart(suffix)
}

func recoveryDeploymentID(operation string, appName string, releaseID string, now time.Time) string {
	parts := []string{"dep", operation, appName}
	if releaseID != "" {
		parts = append(parts, releaseID)
	}
	parts = append(parts, now.UTC().Format("20060102150405_000000000"))

	sanitized := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := sanitizeIDPart(part); value != "" {
			sanitized = append(sanitized, value)
		}
	}
	return strings.Join(sanitized, "_")
}

func routesFromDomains(domains []appmodel.Domain) []router.Route {
	routes := make([]router.Route, 0, len(domains))
	for _, domain := range domains {
		routes = append(routes, router.Route{
			AppID:       domain.AppID,
			ServiceName: domain.ServiceName,
			DomainName:  domain.DomainName,
			Port:        domain.Port,
			HTTPS:       domain.HTTPS,
		})
	}
	return routes
}

func sanitizeIDPart(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}

	return strings.Trim(builder.String(), "_")
}

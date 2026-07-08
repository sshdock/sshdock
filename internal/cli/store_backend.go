package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/sshaccess"
	"github.com/sshdock/sshdock/internal/store"
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
	RecoveryCheckout            appmodel.WorktreeCheckout
	ConfigManager               configManager
	Now                         func() time.Time
}

type routeSyncer interface {
	SyncRoutes(ctx context.Context, routes []router.Route) error
}

type logStreamer interface {
	StreamLogs(ctx context.Context, request compose.LogsRequest, stdout io.Writer, stderr io.Writer) error
}

type configManager interface {
	Set(ctx context.Context, request appconfig.SetRequest) error
	List(ctx context.Context, appID string) ([]appconfig.Entry, error)
	Reveal(ctx context.Context, ref appconfig.ConfigRef) (string, error)
	Unset(ctx context.Context, ref appconfig.ConfigRef) error
	ResolveAppConfig(ctx context.Context, appID string, projectDir string) (map[string]string, error)
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
	recoveryCheckout            appmodel.WorktreeCheckout
	configManager               configManager
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
		recoveryCheckout:            cfg.RecoveryCheckout,
		configManager:               cfg.ConfigManager,
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

	result := cliApp(model)
	if baseDomain, ok := b.currentBaseDomain(ctx); ok {
		if appHost, err := domaincfg.AppHost(name, baseDomain); err == nil {
			result.DefaultURL = "https://" + appHost
		}
	}
	return result, repo.RemoteURL, nil
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

func (b *StoreBackend) RemoveApp(name string) error {
	ctx := context.Background()
	model, err := b.store.GetApp(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("app %q not found", name)
	}
	if err != nil {
		return fmt.Errorf("get app %q: %w", name, err)
	}

	if b.recoveryRunner != nil {
		if release, ok, err := b.latestRuntimeRelease(ctx, name); err != nil {
			return fmt.Errorf("list releases for app removal: %w", err)
		} else if ok && release.ComposePath != "" {
			if err := b.recoveryRunner.Remove(ctx, compose.RemoveRequest{
				AppName:     name,
				ProjectDir:  projectDirFromModel(model, release),
				ComposePath: release.ComposePath,
			}); err != nil {
				return fmt.Errorf("remove Compose project for app %q: %w", name, err)
			}
		}
	}

	if err := b.removeManagedPath(model.RepoPath, "repo"); err != nil {
		return err
	}
	if err := b.removeManagedPath(model.WorktreePath, "worktree"); err != nil {
		return err
	}
	if b.appsDir != "" {
		if err := b.removeManagedPath(filepath.Join(b.appsDir, name), "app directory"); err != nil {
			return err
		}
	}

	if err := b.store.DeleteApp(ctx, name); err != nil {
		return fmt.Errorf("delete app %q state: %w", name, err)
	}
	if err := b.syncRoutesFromStore(ctx); err != nil {
		return fmt.Errorf("reload Caddy routes after app removal: %w", err)
	}

	return nil
}

func (b *StoreBackend) Logs(request LogRequest, stdout io.Writer, stderr io.Writer) error {
	ctx := context.Background()
	if b.recoveryRunner == nil {
		return fmt.Errorf("compose runner is not configured")
	}
	model, err := b.store.GetApp(ctx, request.AppName)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("app %q not found", request.AppName)
	}
	if err != nil {
		return fmt.Errorf("get app %q: %w", request.AppName, err)
	}
	release, ok, err := b.latestRuntimeRelease(ctx, request.AppName)
	if err != nil {
		return fmt.Errorf("list releases for logs: %w", err)
	}
	if !ok || release.ComposePath == "" {
		return fmt.Errorf("no deployed release for app %q", request.AppName)
	}
	projectDir := projectDirFromModel(model, release)
	env, err := b.configEnv(ctx, request.AppName, projectDir)
	if err != nil {
		return err
	}
	logsRequest := compose.LogsRequest{
		AppName:     request.AppName,
		ProjectDir:  projectDir,
		ComposePath: release.ComposePath,
		ServiceName: request.ServiceName,
		Lines:       100,
		Follow:      request.Follow,
		Env:         env,
	}
	redactionValues, err := b.configRedactionValues(ctx, request.AppName)
	if err != nil {
		return err
	}
	if request.Follow {
		if streamer, ok := b.recoveryRunner.(logStreamer); ok {
			if err := streamer.StreamLogs(ctx, logsRequest, redactingWriter{target: stdout, values: redactionValues}, redactingWriter{target: stderr, values: redactionValues}); err != nil {
				return fmt.Errorf("stream logs for app %q: %w", request.AppName, err)
			}
			return nil
		}
	}

	output, err := b.recoveryRunner.Logs(ctx, logsRequest)
	if err != nil {
		return fmt.Errorf("load logs for app %q: %w", request.AppName, err)
	}
	_, err = fmt.Fprint(stdout, compose.RedactValues(output, redactionValues))
	return err

}

func (b *StoreBackend) ListReleases(appName string) ([]Release, error) {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, appName); errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("app %q not found", appName)
	} else if err != nil {
		return nil, fmt.Errorf("get app %q: %w", appName, err)
	}
	models, err := b.store.ListReleasesByApp(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list releases for app %q: %w", appName, err)
	}
	deployments, err := b.store.ListDeploymentsByApp(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list deployments for app %q: %w", appName, err)
	}
	failuresByRelease := map[string]string{}
	for _, deployment := range deployments {
		if deployment.Status == appmodel.DeploymentStatusFailed && deployment.ErrorMessage != "" {
			failuresByRelease[deployment.ReleaseID] = deployment.ErrorMessage
		}
	}

	releases := make([]Release, 0, len(models))
	for _, model := range models {
		releases = append(releases, Release{
			ID:          model.ID,
			AppName:     model.AppID,
			CommitSHA:   model.CommitSHA,
			ComposePath: model.ComposePath,
			Status:      string(model.Status),
			Failure:     failuresByRelease[model.ID],
			CreatedAt:   model.CreatedAt,
			UpdatedAt:   model.UpdatedAt,
		})
	}
	return releases, nil
}

func (b *StoreBackend) ListEvents(appName string) ([]Event, error) {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, appName); errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("app %q not found", appName)
	} else if err != nil {
		return nil, fmt.Errorf("get app %q: %w", appName, err)
	}
	models, err := b.store.ListEventsByApp(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list events for app %q: %w", appName, err)
	}

	events := make([]Event, 0, len(models))
	for _, model := range models {
		events = append(events, Event{
			AppName:   model.AppID,
			Type:      model.Type,
			Message:   model.Message,
			CreatedAt: model.CreatedAt,
		})
	}
	return events, nil
}

func (b *StoreBackend) ListDomains(appName string) ([]Domain, error) {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, appName); errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("app %q not found", appName)
	} else if err != nil {
		return nil, fmt.Errorf("get app %q: %w", appName, err)
	}
	models, err := b.store.ListDomainsByApp(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list domains for app %q: %w", appName, err)
	}

	domains := make([]Domain, 0, len(models))
	for _, model := range models {
		domains = append(domains, cliDomain(model))
	}
	return domains, nil
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
		if err := b.syncRoutesFromStore(ctx); err != nil {
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

func (b *StoreBackend) DetachDomain(appName string, domainName string) error {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, appName); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("app %q not found", appName)
	} else if err != nil {
		return fmt.Errorf("get app %q: %w", appName, err)
	}
	model, err := b.store.DeleteDomainByAppAndName(ctx, appName, domainName)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("domain %q not found for app %q", domainName, appName)
	}
	if err != nil {
		return fmt.Errorf("detach domain %q: %w", domainName, err)
	}
	if err := b.store.CreateEvent(ctx, appmodel.Event{
		ID:        eventID(model.ID, "detached"),
		AppID:     model.AppID,
		Type:      "domain.detached",
		Message:   "Detached " + model.DomainName + " from " + model.AppID,
		CreatedAt: b.now(),
	}); err != nil {
		return fmt.Errorf("record domain detach event: %w", err)
	}
	if b.router != nil {
		if err := b.syncRoutesFromStore(ctx); err != nil {
			_ = b.store.CreateEvent(ctx, appmodel.Event{
				ID:        eventID(model.ID, "router_reload_failed_detach"),
				AppID:     model.AppID,
				Type:      "router.reload_failed",
				Message:   "Caddy reload failed after detaching " + model.DomainName + ": " + err.Error(),
				CreatedAt: b.now(),
			})
			return fmt.Errorf("reload Caddy routes: %w", err)
		}
		if err := b.store.CreateEvent(ctx, appmodel.Event{
			ID:        eventID(model.ID, "router_reloaded_detach"),
			AppID:     model.AppID,
			Type:      "router.reloaded",
			Message:   "Reloaded Caddy routes after detaching " + model.DomainName,
			CreatedAt: b.now(),
		}); err != nil {
			return fmt.Errorf("record Caddy reload event: %w", err)
		}
	}

	return nil
}

func (b *StoreBackend) SetServerGitHost(host string) error {
	baseDomain, err := domaincfg.NormalizeBaseDomain(host)
	if err != nil {
		return err
	}

	if err := b.store.SetServerConfig(context.Background(), store.ServerConfig{
		BaseDomain: baseDomain,
		GitHost:    domaincfg.ControlHost(baseDomain),
		UpdatedAt:  b.now(),
	}); err != nil {
		return fmt.Errorf("set server base domain: %w", err)
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

	if err := b.writeAuthorizedKeys(keys); err != nil {
		return err
	}

	return nil
}

func (b *StoreBackend) ListSSHKeys() ([]SSHKey, error) {
	keys, err := b.store.ListSSHKeys(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list SSH keys: %w", err)
	}
	result := make([]SSHKey, 0, len(keys))
	for _, key := range keys {
		result = append(result, SSHKey{Name: key.Name, PublicKey: key.PublicKey, CreatedAt: key.CreatedAt})
	}
	return result, nil
}

func (b *StoreBackend) RemoveSSHKey(name string) error {
	ctx := context.Background()
	if err := b.store.DeleteSSHKey(ctx, name); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("SSH key %q not found", name)
	} else if err != nil {
		return fmt.Errorf("remove SSH key %q: %w", name, err)
	}
	keys, err := b.store.ListSSHKeys(ctx)
	if err != nil {
		return fmt.Errorf("list SSH keys: %w", err)
	}
	if err := b.writeAuthorizedKeys(keys); err != nil {
		return err
	}
	return nil
}

func (b *StoreBackend) SetConfig(appName string, name string, scope string, value []byte) error {
	if b.configManager == nil {
		return fmt.Errorf("config manager is not configured")
	}
	return b.configManager.Set(context.Background(), appconfig.SetRequest{
		AppID:     appName,
		Name:      name,
		Scope:     scope,
		Value:     value,
		MutatedBy: "dashboard",
	})
}

func (b *StoreBackend) ImportConfig(appName string, scope string, input io.Reader) (int, error) {
	if b.configManager == nil {
		return 0, fmt.Errorf("config manager is not configured")
	}
	if input == nil {
		input = strings.NewReader("")
	}
	scanner := bufio.NewScanner(input)
	count := 0
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return count, fmt.Errorf("config import line %d must be KEY=VALUE", lineNumber)
		}
		if err := b.SetConfig(appName, strings.TrimSpace(name), scope, []byte(value)); err != nil {
			return count, err
		}
		count++
	}
	return count, scanner.Err()
}

func (b *StoreBackend) ListConfig(appName string) ([]ConfigEntry, error) {
	if b.configManager == nil {
		return nil, fmt.Errorf("config manager is not configured")
	}
	entries, err := b.configManager.List(context.Background(), appName)
	if err != nil {
		return nil, err
	}
	result := make([]ConfigEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, ConfigEntry{
			Name:          entry.Name,
			Scope:         entry.Scope,
			Status:        entry.Status,
			RedactedValue: entry.RedactedValue,
			UpdatedAt:     entry.UpdatedAt,
			MutatedBy:     entry.MutatedBy,
		})
	}
	return result, nil
}

func (b *StoreBackend) GetConfig(appName string, name string, scope string) (string, error) {
	if b.configManager == nil {
		return "", fmt.Errorf("config manager is not configured")
	}
	return b.configManager.Reveal(context.Background(), appconfig.ConfigRef{AppID: appName, Name: name, Scope: scope})
}

func (b *StoreBackend) UnsetConfig(appName string, name string, scope string) error {
	if b.configManager == nil {
		return fmt.Errorf("config manager is not configured")
	}
	return b.configManager.Unset(context.Background(), appconfig.ConfigRef{AppID: appName, Name: name, Scope: scope})
}

func (b *StoreBackend) configRedactionValues(ctx context.Context, appName string) (map[string]string, error) {
	if b.configManager == nil {
		return nil, nil
	}
	entries, err := b.configManager.List(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("load config metadata for log redaction: %w", err)
	}
	values := map[string]string{}
	for _, entry := range entries {
		value, err := b.configManager.Reveal(ctx, appconfig.ConfigRef{AppID: appName, Name: entry.Name, Scope: entry.Scope})
		if err != nil {
			return nil, fmt.Errorf("load config value %s for log redaction: %w", entry.Name, err)
		}
		values[entry.Name] = value
	}
	return values, nil
}

func (b *StoreBackend) configEnv(ctx context.Context, appName string, projectDir string) (map[string]string, error) {
	if b.configManager == nil {
		return nil, nil
	}
	env, err := b.configManager.ResolveAppConfig(ctx, appName, projectDir)
	if err != nil {
		return nil, fmt.Errorf("resolve config for logs: %w", err)
	}
	return env, nil
}

type redactingWriter struct {
	target io.Writer
	values map[string]string
}

func (w redactingWriter) Write(p []byte) (int, error) {
	if w.target == nil {
		return len(p), nil
	}
	_, err := io.WriteString(w.target, compose.RedactValues(string(p), w.values))
	return len(p), err
}

func cliApp(model appmodel.App) App {
	return App{
		Name:   model.Name,
		Status: string(model.Status),
		NodeID: model.NodeID,
	}
}

func cliDomain(model appmodel.Domain) Domain {
	return Domain{
		AppName:     model.AppID,
		ServiceName: model.ServiceName,
		DomainName:  model.DomainName,
		Port:        model.Port,
		HTTPS:       model.HTTPS,
	}
}

func (b *StoreBackend) recoveryService() *appmodel.Service {
	options := []appmodel.ServiceOption{appmodel.WithClock(b.now)}
	if b.recoveryRunner != nil {
		options = append(options, appmodel.WithRecoveryRunner(b.recoveryRunner))
	}
	if b.recoveryCheckout != nil {
		options = append(options, appmodel.WithWorktreeCheckout(b.recoveryCheckout))
	}
	if b.configManager != nil {
		options = append(options, appmodel.WithConfigResolver(b.configManager))
	}
	return appmodel.NewService(b.store, options...)
}

func (b *StoreBackend) latestRuntimeRelease(ctx context.Context, appName string) (appmodel.Release, bool, error) {
	releases, err := b.store.ListReleasesByApp(ctx, appName)
	if err != nil {
		return appmodel.Release{}, false, err
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].CreatedAt.Equal(releases[j].CreatedAt) {
			return releases[i].ID < releases[j].ID
		}
		return releases[i].CreatedAt.Before(releases[j].CreatedAt)
	})
	for i := len(releases) - 1; i >= 0; i-- {
		if releases[i].Status == appmodel.ReleaseStatusSucceeded || releases[i].Status == appmodel.ReleaseStatusRolledBack {
			return releases[i], true, nil
		}
	}
	if len(releases) > 0 {
		return releases[len(releases)-1], true, nil
	}
	return appmodel.Release{}, false, nil
}

func projectDirFromModel(model appmodel.App, release appmodel.Release) string {
	if model.WorktreePath != "" {
		return model.WorktreePath
	}
	return filepath.Dir(release.ComposePath)
}

func (b *StoreBackend) syncRoutesFromStore(ctx context.Context) error {
	if b.router == nil {
		return nil
	}
	domains, err := b.store.ListDomains(ctx)
	if err != nil {
		return fmt.Errorf("list domains for route rebuild: %w", err)
	}
	return b.router.SyncRoutes(ctx, routesFromDomains(domains))
}

func (b *StoreBackend) writeAuthorizedKeys(keys []store.SSHKey) error {
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

func (b *StoreBackend) removeManagedPath(path string, label string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if b.appsDir == "" {
		return fmt.Errorf("remove %s %q: apps dir is not configured", label, path)
	}
	root := filepath.Clean(b.appsDir)
	cleanPath := filepath.Clean(path)
	rel, err := filepath.Rel(root, cleanPath)
	if err != nil {
		return fmt.Errorf("remove %s %q: %w", label, path, err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("remove %s %q: path is outside apps dir %q", label, path, b.appsDir)
	}
	if err := os.RemoveAll(cleanPath); err != nil {
		return fmt.Errorf("remove %s %q: %w", label, path, err)
	}
	return nil
}

func (b *StoreBackend) currentGitHost(ctx context.Context) string {
	if gitHost, ok := b.persistedGitHost(ctx); ok {
		return gitHost
	}
	return b.gitHost
}

func (b *StoreBackend) persistedGitHost(ctx context.Context) (string, bool) {
	config, err := b.store.GetServerConfig(ctx)
	if err != nil {
		return "", false
	}
	if config.BaseDomain != "" {
		return domaincfg.ControlHost(config.BaseDomain), true
	}
	if config.GitHost != "" {
		return config.GitHost, true
	}
	return "", false
}

func (b *StoreBackend) currentBaseDomain(ctx context.Context) (string, bool) {
	config, err := b.store.GetServerConfig(ctx)
	if err == nil && config.BaseDomain != "" {
		return config.BaseDomain, true
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

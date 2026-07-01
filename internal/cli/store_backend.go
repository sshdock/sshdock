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
	"github.com/iketiunn/rumbase/internal/gitrecv"
	"github.com/iketiunn/rumbase/internal/store"
)

type ReceiveRepoSetupper interface {
	SetupBareRepo(ctx context.Context, appName string) (gitrecv.BareRepo, error)
}

type StoreBackendConfig struct {
	NodeID       string
	AppsDir      string
	GitHost      string
	RepoSetupper ReceiveRepoSetupper
	Now          func() time.Time
}

type StoreBackend struct {
	store        store.Store
	nodeID       string
	appsDir      string
	gitHost      string
	repoSetupper ReceiveRepoSetupper
	now          func() time.Time
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
		store:        persistentStore,
		nodeID:       cfg.NodeID,
		appsDir:      cfg.AppsDir,
		gitHost:      cfg.GitHost,
		repoSetupper: cfg.RepoSetupper,
		now:          cfg.Now,
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
		RemoteURL: fmt.Sprintf("git@%s:%s", b.gitHost, name),
	}
	if b.repoSetupper != nil {
		var err error
		repo, err = b.repoSetupper.SetupBareRepo(ctx, name)
		if err != nil {
			return App{}, "", fmt.Errorf("set up receive repo for app %q: %w", name, err)
		}
		if repo.Path == "" {
			repo.Path = filepath.Join(b.appsDir, name, "repo.git")
		}
		if repo.RemoteURL == "" {
			repo.RemoteURL = fmt.Sprintf("git@%s:%s", b.gitHost, name)
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

	return nil
}

func cliApp(model appmodel.App) App {
	return App{
		Name:   model.Name,
		Status: string(model.Status),
		NodeID: model.NodeID,
	}
}

func domainID(appName string, domainName string) string {
	return "dom_" + sanitizeIDPart(appName) + "_" + sanitizeIDPart(domainName)
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

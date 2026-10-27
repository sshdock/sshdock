package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	appmodel "github.com/sshdock/sshdock/internal/app"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/store"
)

func (b *StoreBackend) CreateApp(name string) (App, string, error) {
	ctx := context.Background()
	gitHost := b.currentGitHost(ctx)
	if err := validateAppName(name, gitHost); err != nil {
		return App{}, "", err
	}
	if _, err := b.store.GetApp(ctx, name); err == nil {
		return App{}, "", fmt.Errorf("app %q already exists", name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return App{}, "", fmt.Errorf("check app %q: %w", name, err)
	}

	repo := gitrecv.BareRepo{
		Path:      filepath.Join(b.appsDir, name, "repo.git"),
		RemoteURL: fmt.Sprintf("git@%s:%s.git", gitHost, name),
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
			repo.RemoteURL = fmt.Sprintf("git@%s:%s.git", gitHost, name)
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

func validateAppName(name string, gitHost string) error {
	return gitrecv.WithAppNameGuidance(domaincfg.ValidateAppName(name), gitHost)
}

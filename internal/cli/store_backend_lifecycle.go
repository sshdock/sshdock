package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

func (b *StoreBackend) StartApp(name string) error {
	ctx := context.Background()
	if err := b.recoveryService().StartApp(ctx, name); err != nil {
		return fmt.Errorf("start app %q: %w", name, err)
	}
	return nil
}

func (b *StoreBackend) StopApp(name string) error {
	ctx := context.Background()
	if err := b.recoveryService().StopApp(ctx, name); err != nil {
		return fmt.Errorf("stop app %q: %w", name, err)
	}
	return nil
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
	deploymentID, err := b.newDeploymentID()
	if err != nil {
		return fmt.Errorf("create redeploy attempt for app %q: %w", name, err)
	}
	if _, err := b.recoveryService().RedeployCurrentMain(ctx, name, deploymentID); err != nil {
		return fmt.Errorf("redeploy app %q: %w", name, err)
	}
	return nil
}

func (b *StoreBackend) RollbackApp(name string, releaseID string) error {
	ctx := context.Background()
	deploymentID, err := b.newDeploymentID()
	if err != nil {
		return fmt.Errorf("create rollback attempt for app %q: %w", name, err)
	}
	if _, err := b.recoveryService().RollbackRelease(ctx, name, releaseID, deploymentID); err != nil {
		return fmt.Errorf("rollback app %q to %q: %w", name, releaseID, err)
	}
	return nil
}

func (b *StoreBackend) RemoveApp(name string) error {
	ctx := context.Background()
	model, err := b.store.GetApp(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return b.resumeRemoval(ctx, name)
	}
	if err != nil {
		return fmt.Errorf("get app %q: %w", name, err)
	}
	audit, err := b.beginRemovalAudit(ctx, name)
	if err != nil {
		return err
	}
	if err := b.scrubRetainedEventMessages(ctx, name); err != nil {
		return audit.fail("redact retained events", err)
	}

	if b.recoveryRunner != nil {
		if release, ok, err := b.latestRuntimeRelease(ctx, name); err != nil {
			return audit.fail("list Compose releases", err)
		} else if ok && release.ComposePath != "" {
			projectDir := projectDirFromModel(model, release)
			env, err := b.configEnv(ctx, name, projectDir)
			if err != nil {
				return audit.fail("load app config", err)
			}
			if err := b.recoveryRunner.Remove(ctx, compose.RemoveRequest{
				AppName:     name,
				ProjectDir:  projectDir,
				ComposePath: release.ComposePath,
				Env:         env,
			}); err != nil {
				return audit.fail("stop and remove Compose project", err)
			}
		}
	}

	if err := b.removeManagedPath(model.RepoPath, "repo"); err != nil {
		return audit.fail("remove repository", err)
	}
	if err := b.removeManagedPath(model.WorktreePath, "worktree"); err != nil {
		return audit.fail("remove worktree", err)
	}
	if b.appsDir != "" {
		if err := b.removeManagedPath(filepath.Join(b.appsDir, name), "app directory"); err != nil {
			return audit.fail("remove app directory", err)
		}
	}

	if err := b.store.DeleteApp(ctx, name); err != nil {
		return audit.fail("delete app state", err)
	}
	if err := b.syncRoutesFromStore(ctx); err != nil {
		return audit.fail("reload Caddy routes", err)
	}
	if err := b.store.ClearRouteApplyFailures(ctx); err != nil {
		return audit.fail("clear resolved route failures", err)
	}

	return audit.succeed()
}

func (b *StoreBackend) resumeRemoval(ctx context.Context, name string) error {
	events, err := b.store.ListEventsByApp(ctx, name)
	if err != nil {
		return fmt.Errorf("list removal events for app %q: %w", name, err)
	}
	lastRemovalType := ""
	for _, event := range events {
		if strings.HasPrefix(event.Type, "remove.") {
			lastRemovalType = event.Type
		}
	}
	if lastRemovalType != "remove.started" && lastRemovalType != "remove.failed" {
		return fmt.Errorf("app %q not found", name)
	}
	audit, err := b.beginRemovalAudit(ctx, name)
	if err != nil {
		return err
	}
	if err := b.syncRoutesFromStore(ctx); err != nil {
		return audit.fail("reload Caddy routes", err)
	}
	if err := b.store.ClearRouteApplyFailures(ctx); err != nil {
		return audit.fail("clear resolved route failures", err)
	}
	return audit.succeed()
}

func (b *StoreBackend) recoveryService() *appmodel.Service {
	options := []appmodel.ServiceOption{appmodel.WithClock(b.now)}
	if b.recoveryRunner != nil {
		options = append(options, appmodel.WithRecoveryRunner(b.recoveryRunner))
	}
	if b.recoveryCheckout != nil {
		options = append(options, appmodel.WithWorktreeCheckout(b.recoveryCheckout))
	}
	if b.currentMainResolver != nil {
		options = append(options, appmodel.WithCurrentMainResolver(b.currentMainResolver))
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

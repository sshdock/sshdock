package deployment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

type redeployStore interface {
	GetApp(context.Context, string) (app.App, error)
	GetReleaseByAppCommit(context.Context, string, string) (app.Release, error)
	QueueRedeployment(context.Context, app.Deployment) error
	ListDeploymentsByApp(context.Context, string) ([]app.Deployment, error)
}

type RedeployConfig struct {
	Store               redeployStore
	CurrentMainResolver app.CurrentMainResolver
	Now                 func() time.Time
	Poll                func(context.Context) error
}

type Redeployer struct {
	store       redeployStore
	currentMain app.CurrentMainResolver
	now         func() time.Time
	poll        func(context.Context) error
}

func NewRedeployer(config RedeployConfig) *Redeployer {
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Poll == nil {
		config.Poll = func(ctx context.Context) error {
			timer := time.NewTimer(250 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	return &Redeployer{store: config.Store, currentMain: config.CurrentMainResolver, now: config.Now, poll: config.Poll}
}

// RedeployCurrentMain fixes the revision, queues it for the daemon, and waits for that attempt.
func (r *Redeployer) RedeployCurrentMain(ctx context.Context, appID, deploymentID string) error {
	if r.store == nil {
		return fmt.Errorf("redeploy store is not configured")
	}
	if appID == "" || deploymentID == "" {
		return fmt.Errorf("redeploy requires an app and deployment ID")
	}
	if r.currentMain == nil {
		return fmt.Errorf("resolve remote main for app %q: Git resolver is unavailable; run sudo sshdock diagnostics", appID)
	}
	model, err := r.store.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	commitSHA, err := r.currentMain.ResolveCurrentMain(ctx, model.RepoPath)
	if err != nil {
		return fmt.Errorf("resolve remote main for app %q: %w", appID, err)
	}
	if commitSHA == "" {
		return fmt.Errorf("resolve remote main for app %q: commit is empty; run sudo sshdock diagnostics", appID)
	}
	releaseID := app.ReleaseID(appID, commitSHA)
	release, err := r.store.GetReleaseByAppCommit(ctx, appID, commitSHA)
	if err == nil {
		releaseID = release.ID
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	attempt := app.Deployment{ID: deploymentID, AppID: appID, ReleaseID: releaseID, CommitSHA: commitSHA,
		Trigger: app.DeploymentTriggerRedeploy, Status: app.DeploymentStatusPending, StartedAt: r.now()}
	if err := r.store.QueueRedeployment(ctx, attempt); err != nil {
		return fmt.Errorf("queue redeploy for app %q: %w", appID, err)
	}
	return r.wait(ctx, appID, deploymentID)
}

func (r *Redeployer) wait(ctx context.Context, appID, deploymentID string) error {
	for {
		attempts, err := r.store.ListDeploymentsByApp(ctx, appID)
		if err != nil {
			return redeployAttachmentError(appID, deploymentID, err)
		}
		var selected *app.Deployment
		for _, attempt := range attempts {
			if attempt.ID == deploymentID {
				selected = &attempt
				break
			}
		}
		if selected == nil {
			return redeployAttachmentError(appID, deploymentID, store.ErrNotFound)
		}
		switch selected.Status {
		case app.DeploymentStatusSucceeded:
			if selected.ErrorMessage != "" {
				return fmt.Errorf("deployment %s succeeded with an outcome recording error: %s; inspect sudo sshdock deployments info %s %s", deploymentID, selected.ErrorMessage, appID, deploymentID)
			}
			return nil
		case app.DeploymentStatusFailed:
			detail := selected.FailureDetail
			if detail == "" {
				detail = selected.ErrorMessage
			}
			return fmt.Errorf("deployment %s failed: %s; inspect sudo sshdock deployments info %s %s", deploymentID, detail, appID, deploymentID)
		}
		if err := r.poll(ctx); err != nil {
			return redeployAttachmentError(appID, deploymentID, err)
		}
	}
}

func redeployAttachmentError(appID, deploymentID string, err error) error {
	return fmt.Errorf("stopped waiting for deployment %s; daemon deployment continues; reconnect with sudo sshdock deployments logs %s %s -f: %w", deploymentID, appID, deploymentID, err)
}

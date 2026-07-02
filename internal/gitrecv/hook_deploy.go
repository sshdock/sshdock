package gitrecv

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
	"github.com/iketiunn/rumbase/internal/compose"
)

type postReceiveStore interface {
	CreateRelease(ctx context.Context, model app.Release) error
	CreateDeployment(ctx context.Context, model app.Deployment) error
	UpdateAppStatus(ctx context.Context, id string, status app.AppStatus, updatedAt time.Time) error
	UpdateReleaseStatus(ctx context.Context, id string, status app.ReleaseStatus, updatedAt time.Time) error
	UpdateDeploymentStatus(ctx context.Context, id string, status app.DeploymentStatus, finishedAt time.Time, errorMessage string) error
	CreateEvent(ctx context.Context, model app.Event) error
}

type WorktreeCheckout interface {
	Checkout(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error
}

type WorktreeCheckoutFunc func(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error

func (f WorktreeCheckoutFunc) Checkout(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error {
	return f(ctx, repoPath, worktreePath, commitSHA)
}

type PostReceiveHandlerConfig struct {
	Store    postReceiveStore
	Runner   compose.Runner
	Checkout WorktreeCheckout
	Now      func() time.Time
}

type PostReceiveHandler struct {
	store    postReceiveStore
	runner   compose.Runner
	checkout WorktreeCheckout
	now      func() time.Time
}

func NewPostReceiveHandler(config PostReceiveHandlerConfig) *PostReceiveHandler {
	now := config.Now
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}

	return &PostReceiveHandler{
		store:    config.Store,
		runner:   config.Runner,
		checkout: config.Checkout,
		now:      now,
	}
}

func (h *PostReceiveHandler) Handle(ctx context.Context, appName string, repoPath string, worktreePath string, input io.Reader) error {
	if h.store == nil {
		return fmt.Errorf("post-receive store is not configured")
	}
	if h.runner == nil {
		return fmt.Errorf("post-receive compose runner is not configured")
	}
	if h.checkout == nil {
		return fmt.Errorf("post-receive worktree checkout is not configured")
	}

	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		event, err := ParsePostReceiveLine(appName, repoPath, line)
		if err != nil {
			return err
		}
		if err := h.handleEvent(ctx, event, worktreePath); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func (h *PostReceiveHandler) handleEvent(ctx context.Context, event PushEvent, worktreePath string) error {
	if err := h.checkout.Checkout(ctx, event.RepoPath, worktreePath, event.CommitSHA); err != nil {
		return err
	}

	composePath, err := compose.DetectFile(worktreePath)
	if err != nil {
		return err
	}
	if _, err := compose.ValidateFile(composePath); err != nil {
		return err
	}

	now := h.now()
	releaseID := ReleaseID(event.CommitSHA)
	deploymentID := DeploymentID(event.CommitSHA)
	if err := h.store.CreateRelease(ctx, app.Release{
		ID:          releaseID,
		AppID:       event.AppName,
		CommitSHA:   event.CommitSHA,
		ComposePath: composePath,
		Status:      app.ReleaseStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		return err
	}
	if err := h.store.UpdateAppStatus(ctx, event.AppName, app.AppStatusDeploying, now); err != nil {
		return err
	}
	if err := h.store.UpdateReleaseStatus(ctx, releaseID, app.ReleaseStatusDeploying, now); err != nil {
		return err
	}
	if err := h.store.CreateDeployment(ctx, app.Deployment{
		ID:        deploymentID,
		AppID:     event.AppName,
		ReleaseID: releaseID,
		Status:    app.DeploymentStatusDeploying,
		StartedAt: now,
	}); err != nil {
		return err
	}
	if err := h.store.CreateEvent(ctx, app.Event{
		ID:        EventID(deploymentID, "started"),
		AppID:     event.AppName,
		Type:      "deploy.started",
		Message:   "Deploy started for release " + releaseID,
		CreatedAt: now,
	}); err != nil {
		return err
	}

	err = h.runner.Deploy(ctx, compose.DeployRequest{
		AppName:      event.AppName,
		ProjectDir:   worktreePath,
		ComposePath:  composePath,
		ReleaseID:    releaseID,
		CommitSHA:    event.CommitSHA,
		ProjectName:  compose.ProjectName(event.AppName),
		KeepReleases: 5,
		CleanupRecorder: cleanupEventRecorder{
			store:        h.store,
			appID:        event.AppName,
			deploymentID: deploymentID,
			now:          h.now,
		},
	})
	if err != nil {
		finishedAt := h.now()
		_ = h.store.UpdateDeploymentStatus(ctx, deploymentID, app.DeploymentStatusFailed, finishedAt, err.Error())
		_ = h.store.UpdateReleaseStatus(ctx, releaseID, app.ReleaseStatusFailed, finishedAt)
		_ = h.store.UpdateAppStatus(ctx, event.AppName, app.AppStatusFailed, finishedAt)
		_ = h.store.CreateEvent(ctx, app.Event{
			ID:        EventID(deploymentID, "failed"),
			AppID:     event.AppName,
			Type:      "deploy.failed",
			Message:   "Deploy failed for release " + releaseID + ": " + err.Error(),
			CreatedAt: finishedAt,
		})
		return err
	}

	finishedAt := h.now()
	if err := h.store.UpdateDeploymentStatus(ctx, deploymentID, app.DeploymentStatusSucceeded, finishedAt, ""); err != nil {
		return err
	}
	if err := h.store.UpdateReleaseStatus(ctx, releaseID, app.ReleaseStatusSucceeded, finishedAt); err != nil {
		return err
	}
	if err := h.store.UpdateAppStatus(ctx, event.AppName, app.AppStatusHealthy, finishedAt); err != nil {
		return err
	}
	return h.store.CreateEvent(ctx, app.Event{
		ID:        EventID(deploymentID, "succeeded"),
		AppID:     event.AppName,
		Type:      "deploy.succeeded",
		Message:   "Deploy succeeded for release " + releaseID,
		CreatedAt: finishedAt,
	})
}

func ReleaseID(commitSHA string) string {
	return "rel_" + shortCommitSHA(commitSHA)
}

func DeploymentID(commitSHA string) string {
	return "dep_" + shortCommitSHA(commitSHA)
}

func EventID(deploymentID string, suffix string) string {
	return "evt_" + deploymentID + "_" + suffix
}

type cleanupEventRecorder struct {
	store        postReceiveStore
	appID        string
	deploymentID string
	now          func() time.Time
}

func (r cleanupEventRecorder) RecordCleanupFailure(ctx context.Context, failure compose.CleanupFailure) error {
	return r.store.CreateEvent(ctx, app.Event{
		ID:        EventID(r.deploymentID+"_"+failure.ServiceName+"_"+failure.CommitSHA, "cleanup_failed"),
		AppID:     r.appID,
		Type:      "cleanup.failed",
		Message:   "Cleanup failed for image " + failure.Image + ": " + failure.ErrorMessage,
		CreatedAt: r.now(),
	})
}

func shortCommitSHA(commitSHA string) string {
	if len(commitSHA) <= 12 {
		return commitSHA
	}
	return commitSHA[:12]
}

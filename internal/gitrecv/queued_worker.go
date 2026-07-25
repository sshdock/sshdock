package gitrecv

import (
	"context"
	"fmt"

	"github.com/sshdock/sshdock/internal/app"
)

type queuedDeploymentStore interface {
	ClaimNextPendingDeployment(ctx context.Context) (app.Deployment, bool, error)
	GetApp(ctx context.Context, id string) (app.App, error)
}

type queuedDeploymentExecutor interface {
	DeployQueued(ctx context.Context, event PushEvent, worktreePath string, deployment app.Deployment) error
}

type QueuedDeploymentWorkerConfig struct {
	Store    queuedDeploymentStore
	Executor queuedDeploymentExecutor
}

type QueuedDeploymentWorker struct {
	store    queuedDeploymentStore
	executor queuedDeploymentExecutor
}

func NewQueuedDeploymentWorker(config QueuedDeploymentWorkerConfig) *QueuedDeploymentWorker {
	return &QueuedDeploymentWorker{store: config.Store, executor: config.Executor}
}

func (w *QueuedDeploymentWorker) RunOne(ctx context.Context) (bool, error) {
	if w.store == nil {
		return false, fmt.Errorf("queued deployment store is not configured")
	}
	if w.executor == nil {
		return false, fmt.Errorf("queued deployment executor is not configured")
	}
	deployment, found, err := w.store.ClaimNextPendingDeployment(ctx)
	if err != nil || !found {
		return found, err
	}
	model, err := w.store.GetApp(ctx, deployment.AppID)
	if err != nil {
		return true, fmt.Errorf("load app %q for queued deployment %q: %w", deployment.AppID, deployment.ID, err)
	}
	event := PushEvent{AppName: deployment.AppID, RepoPath: model.RepoPath, Branch: "main", CommitSHA: deployment.CommitSHA}
	if err := w.executor.DeployQueued(ctx, event, model.WorktreePath, deployment); err != nil {
		return true, fmt.Errorf("run queued deployment %q: %w", deployment.ID, err)
	}
	return true, nil
}

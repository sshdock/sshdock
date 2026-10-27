package gitrecv

import (
	"context"
	"errors"
	"fmt"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/deploycoord"
)

type queuedDeploymentStore interface {
	pendingDeploymentStore
	ClaimNextPendingDeployment(ctx context.Context) (app.Deployment, bool, error)
	GetApp(ctx context.Context, id string) (app.App, error)
}

type queuedDeploymentExecutor interface {
	Execute(ctx context.Context, deployment app.Deployment) error
}

type QueuedDeploymentWorkerConfig struct {
	Store               queuedDeploymentStore
	Executor            queuedDeploymentExecutor
	LocksDir            string
	CurrentMainResolver app.CurrentMainResolver
}

type QueuedDeploymentWorker struct {
	store        queuedDeploymentStore
	executor     queuedDeploymentExecutor
	coordination *deploycoord.Manager
	currentMain  app.CurrentMainResolver
}

func NewQueuedDeploymentWorker(config QueuedDeploymentWorkerConfig) *QueuedDeploymentWorker {
	currentMain := config.CurrentMainResolver
	if currentMain == nil {
		currentMain = LocalCurrentMainResolver{}
	}
	var coordination *deploycoord.Manager
	if config.LocksDir != "" {
		coordination = deploycoord.NewManager(config.LocksDir)
	}
	return &QueuedDeploymentWorker{store: config.Store, executor: config.Executor,
		coordination: coordination, currentMain: currentMain}
}

func (w *QueuedDeploymentWorker) RunOne(ctx context.Context) (bool, error) {
	if w.store == nil {
		return false, fmt.Errorf("queued deployment store is not configured")
	}
	if w.executor == nil {
		return false, fmt.Errorf("queued deployment executor is not configured")
	}
	recoveryErr := w.recoverPending(ctx)
	deployment, found, err := w.store.ClaimNextPendingDeployment(ctx)
	if err != nil || !found {
		return found, errors.Join(recoveryErr, err)
	}
	if err := w.executor.Execute(ctx, deployment); err != nil {
		return true, errors.Join(recoveryErr, fmt.Errorf("run queued deployment %q: %w", deployment.ID, err))
	}
	return true, recoveryErr
}

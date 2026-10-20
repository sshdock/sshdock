package gitrecv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/deploycoord"
)

type pendingDeploymentStore interface {
	ListUnqueuedPendingDeployments(ctx context.Context) ([]app.Deployment, error)
	RecordDeploymentQueued(ctx context.Context, accepted app.Event, queued app.Event) error
}

// Call only while holding the app receive lock: a reservation alone does not prove Git accepted it.
func recoverPendingDeployments(ctx context.Context, db pendingDeploymentStore, resolver app.CurrentMainResolver, model app.App) error {
	deployments, err := db.ListUnqueuedPendingDeployments(ctx)
	if err != nil {
		return fmt.Errorf("list unqueued pending deployments: %w", err)
	}
	for _, deployment := range deployments {
		if deployment.AppID != model.ID {
			continue
		}
		main, err := resolver.ResolveCurrentMain(ctx, model.RepoPath)
		if errors.Is(err, ErrMainNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("verify remote main for pending deployment %q: %w", deployment.ID, err)
		}
		if main != deployment.CommitSHA {
			continue
		}
		now := time.Now().UTC()
		if err := db.RecordDeploymentQueued(ctx,
			app.Event{ID: EventID(deployment.ID, "git_ref_accepted"), AppID: deployment.AppID, Type: "git.ref_accepted", Message: "Remote main accepted at " + main, CreatedAt: now},
			app.Event{ID: EventID(deployment.ID, "queued"), AppID: deployment.AppID, Type: "deploy.queued", Message: "Deploy queued for release " + deployment.ReleaseID, CreatedAt: now},
		); err != nil {
			return fmt.Errorf("recover pending deployment %q: %w", deployment.ID, err)
		}
	}
	return nil
}

func (w *QueuedDeploymentWorker) recoverPending(ctx context.Context) error {
	deployments, err := w.store.ListUnqueuedPendingDeployments(ctx)
	if err != nil {
		return fmt.Errorf("list unqueued pending deployments: %w", err)
	}
	var recoveryErr error
	for _, deployment := range deployments {
		if w.coordination == nil {
			return fmt.Errorf("pending deployment recovery requires the configured receive lock directory")
		}
		guard, err := w.coordination.AcquireApp(ctx, deployment.AppID)
		if errors.Is(err, deploycoord.ErrAppBusy) {
			continue
		}
		if err != nil {
			recoveryErr = errors.Join(recoveryErr, err)
			continue
		}
		model, err := w.store.GetApp(ctx, deployment.AppID)
		if err == nil {
			err = recoverPendingDeployments(ctx, w.store, w.currentMain, model)
		}
		recoveryErr = errors.Join(recoveryErr, err, guard.Release())
	}
	return recoveryErr
}

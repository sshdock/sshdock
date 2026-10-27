package gitrecv

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
)

func TestQueuedDeploymentWorkerClaimsPendingDeploymentForExecution(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	queue := NewPushQueueHandler(PushQueueHandlerConfig{
		Store:           sqlite,
		NewDeploymentID: func() (string, error) { return "dep_queued", nil },
	})
	if _, err := queue.Queue(ctx, "my-app", strings.NewReader("oldsha abc123 refs/heads/main\n")); err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if err := sqlite.RecordDeploymentQueued(ctx,
		app.Event{ID: EventID("dep_queued", "git_ref_accepted"), AppID: "my-app", Type: "git.ref_accepted", Message: "Remote main accepted"},
		app.Event{ID: EventID("dep_queued", "queued"), AppID: "my-app", Type: "deploy.queued", Message: "Deploy queued"},
	); err != nil {
		t.Fatalf("RecordDeploymentQueued: %v", err)
	}
	executor := &recordingQueuedDeploymentExecutor{}
	worker := NewQueuedDeploymentWorker(QueuedDeploymentWorkerConfig{Store: sqlite, Executor: executor})

	// When
	worked, err := worker.RunOne(ctx)

	// Then
	if err != nil {
		t.Fatalf("RunOne: %v", err)
	}
	if !worked {
		t.Fatal("RunOne worked = false, want claimed deployment")
	}
	if executor.deployment.ID != "dep_queued" || executor.deployment.Status != app.DeploymentStatusDeploying {
		t.Fatalf("executor deployment = %#v, want claimed deployment", executor.deployment)
	}

}

type recordingQueuedDeploymentExecutor struct {
	deployment app.Deployment
}

func (e *recordingQueuedDeploymentExecutor) Execute(_ context.Context, deployment app.Deployment) error {
	e.deployment = deployment
	return nil
}

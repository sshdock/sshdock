package gitrecv

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestPostReceiveHandlerStoresRedactedDeploymentOutputAcrossWrites(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	queue := NewPushQueueHandler(PushQueueHandlerConfig{
		Store:           sqlite,
		NewDeploymentID: func() (string, error) { return "dep_output", nil },
	})
	if _, err := queue.Queue(ctx, "my-app", strings.NewReader("oldsha abc123 refs/heads/main\n")); err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if err := sqlite.RecordDeploymentQueued(ctx,
		app.Event{ID: EventID("dep_output", "git_ref_accepted"), AppID: "my-app", Type: "git.ref_accepted", Message: "Remote main accepted", CreatedAt: time.Now().UTC()},
		app.Event{ID: EventID("dep_output", "queued"), AppID: "my-app", Type: "deploy.queued", Message: "Deploy queued", CreatedAt: time.Now().UTC()},
	); err != nil {
		t.Fatalf("RecordDeploymentQueued: %v", err)
	}
	deployment, found, err := sqlite.ClaimNextPendingDeployment(ctx)
	if err != nil || !found {
		t.Fatalf("ClaimNextPendingDeployment = %#v, %v, want claimed deployment", deployment, err)
	}
	secret := "split-secret"
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:          sqlite,
		Runner:         &splitOutputRunner{FakeRunner: &compose.FakeRunner{}, chunks: []string{"before split-", "secret after\n"}},
		ConfigResolver: staticDeploymentConfigResolver{values: map[string]string{"TOKEN": secret}},
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, worktreePath string, _ string) error {
			writeHookComposeFixture(t, worktreePath)
			return nil
		}),
	})

	// When
	err = handler.DeployQueued(ctx, PushEvent{AppName: "my-app", RepoPath: "/apps/my-app/repo.git", CommitSHA: "abc123"}, filepath.Join(t.TempDir(), "worktree"), deployment)

	// Then
	if err != nil {
		t.Fatalf("DeployQueued: %v", err)
	}
	log, err := sqlite.DeploymentLog(ctx, "my-app", deployment.ID)
	if err != nil {
		t.Fatalf("DeploymentLog: %v", err)
	}
	if strings.Contains(log.Content, secret) || !strings.Contains(log.Content, "before <redacted> after") {
		t.Fatalf("deployment log = %q, want split secret redacted", log.Content)
	}
}

type staticDeploymentConfigResolver struct {
	values map[string]string
}

func (r staticDeploymentConfigResolver) ResolveAppConfig(context.Context, string) (map[string]string, error) {
	return nil, nil
}

func (r staticDeploymentConfigResolver) RedactionValues(context.Context, string) (map[string]string, error) {
	return r.values, nil
}

type splitOutputRunner struct {
	*compose.FakeRunner
	chunks []string
}

func (r *splitOutputRunner) DeployWithOutput(ctx context.Context, request compose.DeployRequest, stdout io.Writer, _ io.Writer) (compose.DeployResult, error) {
	for _, chunk := range r.chunks {
		if _, err := io.WriteString(stdout, chunk); err != nil {
			return compose.DeployResult{}, err
		}
	}
	return r.FakeRunner.Deploy(ctx, request)
}

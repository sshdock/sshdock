package gitrecv

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestPushQueueHandlerQueuesPendingDeploymentBeforeRefUpdate(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	handler := NewPushQueueHandler(PushQueueHandlerConfig{
		Store:           sqlite,
		Now:             func() time.Time { return now },
		NewDeploymentID: func() (string, error) { return "dep_queued", nil },
	})

	// When
	deployment, err := handler.Queue(ctx, "my-app", strings.NewReader("oldsha abc123 refs/heads/main\n"))

	// Then
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	want := app.Deployment{ID: "dep_queued", AppID: "my-app", ReleaseID: app.ReleaseID("my-app", "abc123"), CommitSHA: "abc123", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now}
	if deployment != want {
		t.Fatalf("deployment = %#v, want %#v", deployment, want)
	}
	deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 1 || deployments[0] != want {
		t.Fatalf("deployments = %#v, want [%#v]", deployments, want)
	}
}

func TestPostReceiveHandlerDeploysClaimedQueuedDeployment(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	queue := NewPushQueueHandler(PushQueueHandlerConfig{
		Store:           sqlite,
		NewDeploymentID: func() (string, error) { return "dep_queued", nil },
	})
	if _, err := queue.Queue(ctx, "my-app", strings.NewReader("oldsha abc123 refs/heads/main\n")); err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if err := sqlite.RecordDeploymentQueued(ctx,
		app.Event{ID: EventID("dep_queued", "git_ref_accepted"), AppID: "my-app", Type: "git.ref_accepted", Message: "Remote main accepted", CreatedAt: time.Now().UTC()},
		app.Event{ID: EventID("dep_queued", "queued"), AppID: "my-app", Type: "deploy.queued", Message: "Deploy queued", CreatedAt: time.Now().UTC()},
	); err != nil {
		t.Fatalf("RecordDeploymentQueued: %v", err)
	}
	claimed, found, err := sqlite.ClaimNextPendingDeployment(ctx)
	if err != nil {
		t.Fatalf("ClaimNextPendingDeployment: %v", err)
	}
	if !found {
		t.Fatal("ClaimNextPendingDeployment found = false, want true")
	}
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: &compose.FakeRunner{},
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeHookComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	// When
	err = handler.DeployQueued(ctx, PushEvent{AppName: "my-app", RepoPath: "/apps/my-app/repo.git", CommitSHA: "abc123"}, worktreePath, claimed)

	// Then
	if err != nil {
		t.Fatalf("DeployQueued: %v", err)
	}
	deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 1 || deployments[0].Status != app.DeploymentStatusSucceeded {
		t.Fatalf("deployments = %#v, want succeeded queued deployment", deployments)
	}
}

func TestPostReceiveQueueHandlerRecordsAcceptedRefAndReportsQueuedDeployment(t *testing.T) {
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
	var output bytes.Buffer
	handler := NewPostReceiveQueueHandler(PostReceiveQueueHandlerConfig{Store: sqlite, Output: &output})

	// When
	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", strings.NewReader("oldsha abc123 refs/heads/main\n"))

	// Then
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	for _, want := range []string{"git: remote main updated to abc123", "deploy: queued dep_queued for current main abc123"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if len(events) != 2 || events[0].Type != "git.ref_accepted" || events[1].Type != "deploy.queued" {
		t.Fatalf("events = %#v, want accepted-ref and queued events", events)
	}
}

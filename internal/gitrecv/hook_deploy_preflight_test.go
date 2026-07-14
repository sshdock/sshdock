package gitrecv

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestPostReceiveHandlerRecordsCheckoutFailureAsDeploymentAttempt(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	failure := errors.New("checkout failed")
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: &compose.FakeRunner{},
		Checkout: WorktreeCheckoutFunc(func(context.Context, string, string, string) error {
			return failure
		}),
		NewDeploymentID: func() (string, error) { return "dep_checkout", nil },
	})

	// When
	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", filepath.Join(t.TempDir(), "worktree"), strings.NewReader("old abc123 refs/heads/main\n"))

	// Then
	if !errors.Is(err, failure) {
		t.Fatalf("Handle error = %v, want %v", err, failure)
	}
	assertPreflightAttempt(t, ctx, sqlite, "dep_checkout", "abc123", "checkout")
}

func TestPostReceiveHandlerRecordsComposeDetectionFailureAsDeploymentAttempt(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: &compose.FakeRunner{},
		Checkout: WorktreeCheckoutFunc(func(context.Context, string, string, string) error {
			return nil
		}),
		NewDeploymentID: func() (string, error) { return "dep_detect", nil },
	})

	// When
	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", filepath.Join(t.TempDir(), "worktree"), strings.NewReader("old abc123 refs/heads/main\n"))

	// Then
	if err == nil {
		t.Fatal("Handle error = nil, want Compose detection failure")
	}
	assertPreflightAttempt(t, ctx, sqlite, "dep_detect", "abc123", "detect compose")
}

func assertPreflightAttempt(t *testing.T, ctx context.Context, sqlite interface {
	ListDeploymentsByApp(context.Context, string) ([]app.Deployment, error)
	ListEventsByApp(context.Context, string) ([]app.Event, error)
}, deploymentID string, commitSHA string, stage string) {
	t.Helper()
	deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 1 {
		t.Fatalf("deployments = %#v, want one attempt", deployments)
	}
	got := deployments[0]
	if got.ID != deploymentID || got.ReleaseID != app.ReleaseID("my-app", commitSHA) || got.CommitSHA != commitSHA || got.Trigger != app.DeploymentTriggerPush || got.Status != app.DeploymentStatusFailed || got.FailureStage != stage || got.FailureDetail == "" || got.RetryGuidance == "" {
		t.Fatalf("deployment = %#v", got)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if len(events) != 2 || events[0].Type != "deploy.started" || events[1].Type != "deploy.failed" {
		t.Fatalf("events = %#v", events)
	}
}

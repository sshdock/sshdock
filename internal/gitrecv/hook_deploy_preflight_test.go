package gitrecv

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
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

func TestPostReceiveHandlerRecordsNativeRequiredConfigFailureWithoutLeakingFlatValues(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	secret := "postgres://secret"
	configService := appconfig.NewService(sqlite, filepath.Join(t.TempDir(), "config.key"))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte(secret)}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	runner := &compose.FakeRunner{}
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:          sqlite,
		Runner:         runner,
		ConfigResolver: configService,
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, worktreePath string, _ string) error {
			writeHookCompose(t, worktreePath, `
services:
  web:
    image: ${MISSING_IMAGE:?database is ${DATABASE_URL}}
`)
			return nil
		}),
		NewDeploymentID: func() (string, error) { return "dep_required_config", nil },
	})

	// When
	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", filepath.Join(t.TempDir(), "worktree"), strings.NewReader("old abc123 refs/heads/main\n"))

	// Then
	if err == nil || !strings.Contains(err.Error(), "MISSING_IMAGE") || !strings.Contains(err.Error(), "fix compose.yml") {
		t.Fatalf("Handle error = %v, want actionable redacted interpolation failure", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Handle error leaked config value: %v", err)
	}
	if len(runner.DeployRequests) != 0 {
		t.Fatalf("DeployRequests = %#v, want validation failure before deploy", runner.DeployRequests)
	}
	assertPreflightAttempt(t, ctx, sqlite, "dep_required_config", "abc123", string(compose.DeployStageValidateCompose))
	deployments, listErr := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if listErr != nil {
		t.Fatalf("ListDeploymentsByApp: %v", listErr)
	}
	events, listErr := sqlite.ListEventsByApp(ctx, "my-app")
	if listErr != nil {
		t.Fatalf("ListEventsByApp: %v", listErr)
	}
	for _, text := range []string{deployments[0].FailureDetail, events[len(events)-1].Message} {
		if strings.Contains(text, secret) || !strings.Contains(text, "MISSING_IMAGE") {
			t.Fatalf("persisted failure is not redacted: %q", text)
		}
	}
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
	if len(events) != 3 || events[0].Type != "git.ref_accepted" || events[1].Type != "deploy.started" || events[2].Type != "deploy.failed" {
		t.Fatalf("events = %#v", events)
	}
}

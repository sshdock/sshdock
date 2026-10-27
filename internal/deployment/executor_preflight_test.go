package deployment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestExecutorRecordsCheckoutFailureAsDeploymentAttempt(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newExecutorTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	failure := errors.New("checkout failed")
	handler := newTestExecutor(t, Config{
		Store:  sqlite,
		Runner: &compose.FakeRunner{},
		Checkout: executorCheckoutFunc(func(context.Context, string, string, string) error {
			return failure
		}),
	})

	// When
	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_checkout")

	// Then
	if !errors.Is(err, failure) {
		t.Fatalf("Execute error = %v, want %v", err, failure)
	}
	assertPreflightAttempt(t, ctx, sqlite, "dep_checkout", "abc123", "checkout")
}

func TestExecutorProvidesCommitBeforeComposeValidation(t *testing.T) {
	ctx := context.Background()
	sqlite := newExecutorTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	runner := &compose.FakeRunner{}
	handler := newTestExecutor(t, Config{
		Store: sqlite, Runner: runner,
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, worktreePath string, _ string) error {
			writeExecutorCompose(t, worktreePath, "services:\n  web:\n    image: registry.example/app:${SSHDOCK_GIT_SHA:?deploy a commit}\n")
			return nil
		}),
	})
	sha := strings.Repeat("a", 40)
	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", sha, "dep_abc123"); err != nil {
		t.Fatal(err)
	}
	if len(runner.DeployRequests) != 1 || runner.DeployRequests[0].Env[compose.GitSHAEnv] != sha {
		t.Fatalf("deploy requests = %#v", runner.DeployRequests)
	}
}

func TestExecutorUsesAttemptSHAAndPureConfigDespiteInvalidCheckoutHEAD(t *testing.T) {
	ctx := context.Background()
	db, checkout := newPersistenceFixture(t)
	model, err := db.GetApp(ctx, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(model.RepoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(model.RepoPath, "HEAD"), []byte("invalid-head"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := appconfig.NewService(db, filepath.Join(t.TempDir(), "config.key"))
	secret := "stored-secret"
	if err := config.Set(ctx, appconfig.SetRequest{AppID: model.ID, Name: "TOKEN", Value: []byte(secret)}); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	runner := &compose.FakeRunner{DeployOutput: sha + " " + secret + "\n"}
	executor := newTestExecutor(t, Config{Store: db, Runner: runner, Checkout: checkout, ConfigResolver: config})
	if err := executeAttempt(t, ctx, db, executor, model.ID, sha, "dep_exact_attempt"); err != nil {
		t.Fatalf("old checkout HEAD blocked exact attempt: %v", err)
	}
	if len(runner.DeployRequests) != 1 || runner.DeployRequests[0].Env[compose.GitSHAEnv] != sha || runner.DeployRequests[0].Env["TOKEN"] != secret {
		t.Fatalf("deployment requests = %#v", runner.DeployRequests)
	}
	log, err := db.DeploymentLog(ctx, model.ID, "dep_exact_attempt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log.Content, secret) || !strings.Contains(log.Content, sha+" <redacted>") {
		t.Fatalf("durable output = %q", log.Content)
	}
}

func TestExecutorRecordsComposeDetectionFailureAsDeploymentAttempt(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newExecutorTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	handler := newTestExecutor(t, Config{
		Store:  sqlite,
		Runner: &compose.FakeRunner{},
		Checkout: executorCheckoutFunc(func(context.Context, string, string, string) error {
			return nil
		}),
	})

	// When
	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_detect")

	// Then
	if err == nil {
		t.Fatal("Execute error = nil, want Compose detection failure")
	}
	assertPreflightAttempt(t, ctx, sqlite, "dep_detect", "abc123", "detect compose")
}

func TestExecutorRecordsNativeRequiredConfigFailureWithoutLeakingFlatValues(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newExecutorTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	secret := "postgres://secret"
	configService := appconfig.NewService(sqlite, filepath.Join(t.TempDir(), "config.key"))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte(secret)}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	runner := &compose.FakeRunner{}
	handler := newTestExecutor(t, Config{
		Store:          sqlite,
		Runner:         runner,
		ConfigResolver: configService,
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, worktreePath string, _ string) error {
			writeExecutorCompose(t, worktreePath, `
services:
  web:
    image: ${MISSING_IMAGE:?database is ${DATABASE_URL}}
`)
			return nil
		}),
	})

	// When
	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_required_config")

	// Then
	if err == nil || !strings.Contains(err.Error(), "MISSING_IMAGE") || !strings.Contains(err.Error(), "fix compose.yml") {
		t.Fatalf("Execute error = %v, want actionable redacted interpolation failure", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Execute error leaked config value: %v", err)
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
	if got.ID != deploymentID || got.ReleaseID != app.ReleaseID("my-app", commitSHA) || got.CommitSHA != commitSHA || got.Trigger != app.DeploymentTriggerRedeploy || got.Status != app.DeploymentStatusFailed || got.FailureStage != stage || got.FailureDetail == "" || got.RetryGuidance == "" {
		t.Fatalf("deployment = %#v", got)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	deployEvents := make([]app.Event, 0, len(events))
	for _, event := range events {
		if strings.HasPrefix(event.Type, "deploy.") {
			deployEvents = append(deployEvents, event)
		}
	}
	if len(deployEvents) != 3 || deployEvents[0].Type != "deploy.queued" || deployEvents[1].Type != "deploy.started" || deployEvents[2].Type != "deploy.failed" {
		t.Fatalf("events = %#v", events)
	}
}

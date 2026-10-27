package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/deployment"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

func TestConfigImportAndGitPushDeployUsesProcessEnvironment(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sqlite, err := store.OpenSQLite(ctx, filepath.Join(root, "sshdock.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer sqlite.Close()

	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	appName := "config-app"
	worktreePath := filepath.Join(root, "apps", appName, "worktree")
	if err := sqlite.CreateApp(ctx, app.App{
		ID:           appName,
		Name:         appName,
		NodeID:       "local",
		RepoPath:     filepath.Join(root, "apps", appName, "repo.git"),
		WorktreePath: worktreePath,
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	configService := appconfig.NewService(sqlite, filepath.Join(root, "config.key"), appconfig.WithClock(func() time.Time { return now }))
	if count, err := configService.Import(ctx, appconfig.ImportRequest{
		AppID: appName,
		Input: strings.NewReader("DATABASE_URL=postgres://secret\n"),
	}); err != nil || count != 1 {
		t.Fatalf("Import config: count=%d err=%v", count, err)
	}

	runner := &compose.FakeRunner{}
	handler := deployment.NewExecutor(deployment.Config{
		Store:          sqlite,
		Routing:        router.NewService(router.ServiceConfig{Store: sqlite, Router: router.NewFakeRouter(), LocksDir: t.TempDir()}),
		Runner:         runner,
		ConfigResolver: configService,
		Checkout: configWorkflowCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			if err := os.MkdirAll(gotWorktreePath, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(gotWorktreePath, "compose.yml"), []byte(`services:
  web:
    image: nginx:alpine
    environment:
      DATABASE_URL: ${DATABASE_URL:?set DATABASE_URL with sshdock config set}
`), 0o644)
		}),
		Now: func() time.Time { return now },
	})

	queue := gitrecv.NewPushQueueHandler(gitrecv.PushQueueHandlerConfig{Store: sqlite, NewDeploymentID: func() (string, error) { return "dep_config", nil }})
	if _, err := queue.Queue(ctx, appName, strings.NewReader("old abc123 refs/heads/main\n")); err != nil {
		t.Fatal(err)
	}
	postReceive := gitrecv.NewPostReceiveQueueHandler(gitrecv.PostReceiveQueueHandlerConfig{Store: sqlite})
	if err := postReceive.Handle(ctx, appName, filepath.Join(filepath.Dir(worktreePath), "repo.git"), strings.NewReader("old abc123 refs/heads/main\n")); err != nil {
		t.Fatal(err)
	}
	attempt, found, err := sqlite.ClaimNextPendingDeployment(ctx)
	if err != nil || !found {
		t.Fatalf("claim deployment: found=%v err=%v", found, err)
	}
	if err := handler.Execute(ctx, attempt); err != nil {
		t.Fatalf("Execute Git attempt: %v", err)
	}

	if len(runner.DeployRequests) != 1 {
		t.Fatalf("DeployRequests = %#v, want one", runner.DeployRequests)
	}
	if runner.DeployRequests[0].Env["DATABASE_URL"] != "postgres://secret" {
		t.Fatalf("deploy env = %#v", runner.DeployRequests[0].Env)
	}

	composeFile, err := os.ReadFile(filepath.Join(worktreePath, "compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile compose: %v", err)
	}
	if strings.Contains(string(composeFile), "postgres://secret") {
		t.Fatalf("compose file contains secret:\n%s", composeFile)
	}
}

type configWorkflowCheckoutFunc func(context.Context, string, string, string) error

func (f configWorkflowCheckoutFunc) Checkout(ctx context.Context, repo, worktree, commit string) error {
	return f(ctx, repo, worktree, commit)
}

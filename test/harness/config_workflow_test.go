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
	"github.com/sshdock/sshdock/internal/gitrecv"
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
	if err := configService.Import(ctx, appconfig.ImportRequest{
		AppID: appName,
		Input: strings.NewReader("DATABASE_URL=postgres://secret\n"),
	}); err != nil {
		t.Fatalf("Import config: %v", err)
	}

	runner := &compose.FakeRunner{}
	handler := gitrecv.NewPostReceiveHandler(gitrecv.PostReceiveHandlerConfig{
		Store:          sqlite,
		Runner:         runner,
		ConfigResolver: configService,
		Checkout: gitrecv.WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
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

	if err := handler.Handle(ctx, appName, filepath.Join(root, "apps", appName, "repo.git"), worktreePath, strings.NewReader("oldsha abc123 refs/heads/main\n")); err != nil {
		t.Fatalf("Handle: %v", err)
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

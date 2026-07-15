package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/store"
)

func TestRunDashboardConfigCommandFeedsGitPushDeploy(t *testing.T) {
	// Given
	ctx := context.Background()
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "sshdock.db")
	keyPath := filepath.Join(dataDir, "config.key")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", dbPath)
	t.Setenv("SSHDOCK_CONFIG_KEY_PATH", keyPath)
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	sqlite, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	if err := sqlite.CreateApp(ctx, appmodel.App{ID: "my-app", Name: "my-app", NodeID: "local", Status: appmodel.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// When
	secret := "postgres://secret"
	t.Setenv("SSH_ORIGINAL_COMMAND", "config set my-app DATABASE_URL")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithInput([]string{"dashboard"}, strings.NewReader(secret+"\n"), &stdout, &stderr)

	// Then
	if code != 0 {
		t.Fatalf("config set exit code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), secret) {
		t.Fatalf("config set leaked secret stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	database, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("ReadFile database: %v", err)
	}
	if bytes.Contains(database, []byte(secret)) {
		t.Fatal("database contains plaintext config value")
	}

	sqlite, err = store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen SQLite: %v", err)
	}
	defer sqlite.Close()
	deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp before push: %v", err)
	}
	if len(deployments) != 0 {
		t.Fatalf("config mutation created deployments: %#v", deployments)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp after config set: %v", err)
	}
	if len(events) != 1 || events[0].Type != "config.set" || strings.Contains(events[0].Message, secret) {
		t.Fatalf("config mutation events = %#v", events)
	}
	configService := appconfig.NewService(sqlite, keyPath)
	value, err := configService.Reveal(ctx, appconfig.ConfigRef{AppID: "my-app", Name: "DATABASE_URL"})
	if err != nil || value != secret {
		t.Fatalf("Reveal = %q, err=%v", value, err)
	}

	runner := &compose.FakeRunner{}
	worktreePath := filepath.Join(dataDir, "apps", "my-app", "worktree")
	handler := gitrecv.NewPostReceiveHandler(gitrecv.PostReceiveHandlerConfig{
		Store:          sqlite,
		Runner:         runner,
		ConfigResolver: configService,
		Checkout: gitrecv.WorktreeCheckoutFunc(func(_ context.Context, _ string, path string, _ string) error {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(path, "compose.yml"), []byte("services:\n  web:\n    image: nginx:alpine\n    environment:\n      DATABASE_URL: ${DATABASE_URL:?set DATABASE_URL}\n"), 0o644)
		}),
	})
	if err := handler.Handle(ctx, "my-app", filepath.Join(dataDir, "apps", "my-app", "repo.git"), worktreePath, strings.NewReader("old abc123 refs/heads/main\n")); err != nil {
		t.Fatalf("Handle Git push: %v", err)
	}
	if len(runner.DeployRequests) != 1 || runner.DeployRequests[0].Env["DATABASE_URL"] != secret {
		t.Fatalf("DeployRequests = %#v", runner.DeployRequests)
	}
	if _, err := os.Stat(filepath.Join(worktreePath, ".sshdock.yml")); !os.IsNotExist(err) {
		t.Fatalf("legacy manifest unexpectedly exists: %v", err)
	}
}

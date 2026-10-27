package diagnostics

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

func TestRunWarnsForRoutedServiceWithoutRestartPolicy(t *testing.T) {
	root := t.TempDir()
	cfg := diagnosticsConfig(root)
	prepareHealthyDiagnosticsRuntime(t, cfg)
	worktreePath := filepath.Join(cfg.AppsDir, "my-app", "worktree")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte("services:\n  web:\n    image: example/web\n"), 0o644); err != nil {
		t.Fatalf("WriteFile Compose: %v", err)
	}
	repoPath := filepath.Join(cfg.AppsDir, "my-app", "repo.git")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("MkdirAll repository: %v", err)
	}
	sha := strings.Repeat("a", 40)
	if err := os.WriteFile(filepath.Join(repoPath, "HEAD"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile HEAD: %v", err)
	}
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	db, err := store.OpenSQLite(context.Background(), cfg.SQLiteDBPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := db.CreateApp(context.Background(), appmodel.App{ID: "my-app", Name: "my-app", RepoPath: repoPath, WorktreePath: worktreePath, Status: appmodel.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := appconfig.NewService(db, cfg.ConfigKeyPath).Set(context.Background(), appconfig.SetRequest{AppID: "my-app", Name: "API_TOKEN", Value: []byte("diagnostic-secret")}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	if err := db.AttachDomain(context.Background(), appmodel.Domain{ID: "dom_1", AppID: "my-app", ServiceName: "web", DomainName: "app.example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close SQLite: %v", err)
	}

	executor := healthyExecutor(cfg)
	report := Run(context.Background(), cfg, executor)

	if !report.OK {
		t.Fatalf("report OK = false, checks = %#v", report.Checks)
	}
	if !strings.Contains(report.String(), "warn restart policy my-app:") || !strings.Contains(report.String(), "web") {
		t.Fatalf("report missing restart policy warning:\n%s", report.String())
	}
	composeCalls := 0
	for _, command := range executor.Commands {
		if command.Name != "docker" || command.Dir != worktreePath {
			continue
		}
		composeCalls++
		if command.Env["API_TOKEN"] != "diagnostic-secret" || command.Env[compose.GitSHAEnv] != sha {
			t.Fatalf("Compose environment missing runtime config or checked-out revision: %#v", command.Env)
		}
	}
	if composeCalls == 0 {
		t.Fatal("restart policy check did not inspect Compose")
	}
}

func TestRunWarnsForRunningUnroutedServiceWithoutRestartPolicy(t *testing.T) {
	root := t.TempDir()
	cfg := diagnosticsConfig(root)
	prepareHealthyDiagnosticsRuntime(t, cfg)
	worktreePath := filepath.Join(cfg.AppsDir, "worker-app", "worktree")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll worktree: %v", err)
	}
	composePath := filepath.Join(worktreePath, "compose.yml")
	if err := os.WriteFile(composePath, []byte("services:\n  worker:\n    image: example/worker\n"), 0o644); err != nil {
		t.Fatalf("WriteFile Compose: %v", err)
	}
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	db, err := store.OpenSQLite(context.Background(), cfg.SQLiteDBPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := db.CreateApp(context.Background(), appmodel.App{ID: "worker-app", Name: "worker-app", WorktreePath: worktreePath, Status: appmodel.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close SQLite: %v", err)
	}
	executor := healthyExecutor(cfg)
	statusCommand := "docker compose -f " + composePath + " -p sshdock_worker-app ps --all --format json"
	executor.Outputs[statusCommand] = `[{"Service":"worker","State":"running"}]`

	report := Run(context.Background(), cfg, executor)

	if !strings.Contains(report.String(), "warn restart policy worker-app:") || !strings.Contains(report.String(), "worker") {
		t.Fatalf("report missing running-service restart warning:\n%s", report.String())
	}
}

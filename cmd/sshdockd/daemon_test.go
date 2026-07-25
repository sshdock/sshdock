package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestRunDaemonContextDoesNotDeployAppsOnStartup(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "sshdock.db")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", dbPath)
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	t.Setenv("SSHDOCK_FAKE_COMPOSE_DEPLOY_ERROR", "startup must not deploy")
	now := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	db, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	if err := db.CreateApp(context.Background(), appmodel.App{ID: "app_1", Name: "app_1", Status: appmodel.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := db.CreateRelease(context.Background(), appmodel.Release{ID: "rel_good", AppID: "app_1", CommitSHA: "abc123", ComposePath: "/apps/app_1/worktree/compose.yml", Status: appmodel.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create release: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close SQLite: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer

	code := runDaemonContext(ctx, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	db, err = store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen SQLite: %v", err)
	}
	defer db.Close()
	deployments, err := db.ListDeploymentsByApp(context.Background(), "app_1")
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(deployments) != 0 {
		t.Fatalf("startup deployments = %#v, want none", deployments)
	}
}

func TestRunDaemonContextMarksInterruptedDeploymentFailedOnStartup(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "sshdock.db")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", dbPath)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	db, err := store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := db.CreateApp(context.Background(), appmodel.App{ID: "app_1", Name: "app_1", Status: appmodel.AppStatusDeploying, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := db.CreateRelease(context.Background(), appmodel.Release{ID: "rel_1", AppID: "app_1", CommitSHA: "abc123", ComposePath: "/apps/app_1/worktree/compose.yml", Status: appmodel.ReleaseStatusDeploying, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	if err := db.CreateDeployment(context.Background(), appmodel.Deployment{ID: "dep_1", AppID: "app_1", ReleaseID: "rel_1", CommitSHA: "abc123", Trigger: appmodel.DeploymentTriggerPush, Status: appmodel.DeploymentStatusDeploying, StartedAt: now}); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if err := db.CreateDeployment(context.Background(), appmodel.Deployment{ID: "dep_redeploy", AppID: "app_1", ReleaseID: "rel_1", CommitSHA: "abc123", Trigger: appmodel.DeploymentTriggerRedeploy, Status: appmodel.DeploymentStatusDeploying, StartedAt: now}); err != nil {
		t.Fatalf("CreateDeployment redeploy: %v", err)
	}
	if err := db.CreateApp(context.Background(), appmodel.App{ID: "app_pending", Name: "app_pending", Status: appmodel.AppStatusDeploying, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateApp pending: %v", err)
	}
	if err := db.CreateDeployment(context.Background(), appmodel.Deployment{ID: "dep_pending", AppID: "app_pending", ReleaseID: "rel_pending", CommitSHA: "pending123", Trigger: appmodel.DeploymentTriggerPush, Status: appmodel.DeploymentStatusPending, StartedAt: now}); err != nil {
		t.Fatalf("CreateDeployment pending: %v", err)
	}
	if err := db.RecordDeploymentQueued(context.Background(),
		appmodel.Event{ID: "evt_dep_pending_git_ref_accepted", AppID: "app_pending", Type: "git.ref_accepted", Message: "Remote main accepted", CreatedAt: now},
		appmodel.Event{ID: "evt_dep_pending_queued", AppID: "app_pending", Type: "deploy.queued", Message: "Deploy queued", CreatedAt: now},
	); err != nil {
		t.Fatalf("RecordDeploymentQueued: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer

	// When
	code := runDaemonContext(ctx, &stderr)

	// Then
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	db, err = store.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen SQLite: %v", err)
	}
	defer db.Close()
	deployments, err := db.ListDeploymentsByApp(context.Background(), "app_1")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 2 || deployments[0].Status != appmodel.DeploymentStatusFailed || deployments[0].FailureStage != "daemon restart" || deployments[0].RetryGuidance == "" {
		t.Fatalf("deployments = %#v, want interrupted push deployment marked failed", deployments)
	}
	if deployments[1].ID != "dep_redeploy" || deployments[1].Status != appmodel.DeploymentStatusDeploying {
		t.Fatalf("deployments = %#v, want independent redeploy attempt untouched", deployments)
	}
	pendingDeployments, err := db.ListDeploymentsByApp(context.Background(), "app_pending")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp pending: %v", err)
	}
	if len(pendingDeployments) != 1 || pendingDeployments[0].Status != appmodel.DeploymentStatusPending {
		t.Fatalf("pending deployments = %#v, want accepted queued deployment retained for the restarted daemon", pendingDeployments)
	}
}

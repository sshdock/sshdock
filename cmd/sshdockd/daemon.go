package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

func runDaemon(stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runDaemonContext(ctx, stderr)
}

func runDaemonContext(ctx context.Context, stderr io.Writer) int {
	cfg := config.LoadFromEnv()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "create data dir: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SQLiteDBPath), 0o755); err != nil {
		fmt.Fprintf(stderr, "create database dir: %v\n", err)
		return 1
	}

	sqlite, err := store.OpenSQLite(context.Background(), cfg.SQLiteDBPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer sqlite.Close()

	if err := recoverInterruptedDeployments(context.Background(), sqlite, func() time.Time { return time.Now().UTC() }); err != nil {
		fmt.Fprintln(stderr, "recover interrupted deployments:", err)
		return 1
	}
	runner, err := hookRunnerFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath)
	executor := gitrecv.NewPostReceiveHandler(gitrecv.PostReceiveHandlerConfig{
		Store:          sqlite,
		Runner:         runner,
		ConfigResolver: configService,
		Router: router.NewCaddyRouter(router.CaddyRouterConfig{
			ConfigPath:   cfg.CaddyConfigPath,
			Executor:     router.LocalCommandExecutor{},
			AdminAddress: cfg.CaddyAdminAddress,
			UpstreamHost: "127.0.0.1",
		}),
		Checkout: gitrecv.LocalWorktreeCheckout{},
		Output:   io.Discard,
	})
	worker := gitrecv.NewQueuedDeploymentWorker(gitrecv.QueuedDeploymentWorkerConfig{Store: sqlite, Executor: executor})
	return runQueuedDeploymentLoop(ctx, worker, stderr)
}

type queuedDeploymentWorker interface {
	RunOne(ctx context.Context) (bool, error)
}

func runQueuedDeploymentLoop(ctx context.Context, worker queuedDeploymentWorker, stderr io.Writer) int {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		default:
		}
		worked, err := worker.RunOne(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "run queued deployment:", err)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
		}
	}
}

func recoverInterruptedDeployments(ctx context.Context, sqlite *store.SQLiteStore, now func() time.Time) error {
	deployments, err := sqlite.ListDeploymentsByStatus(ctx, app.DeploymentStatusDeploying)
	if err != nil {
		return fmt.Errorf("list active deployments: %w", err)
	}
	for _, deployment := range deployments {
		if deployment.Trigger != app.DeploymentTriggerPush {
			continue
		}
		finishedAt := now()
		deployment.Status = app.DeploymentStatusFailed
		deployment.FinishedAt = finishedAt
		deployment.FailureStage = "daemon restart"
		deployment.FailureDetail = "daemon restarted while this deployment was active; it was not automatically rerun"
		deployment.RetryGuidance = "inspect failure detail and push a known-good commit to remote main"
		if err := sqlite.UpdateDeploymentFailure(ctx, deployment); err != nil {
			return fmt.Errorf("mark deployment %q failed: %w", deployment.ID, err)
		}
		if err := sqlite.MarkReleaseFailedUnlessSucceeded(ctx, deployment.ReleaseID, finishedAt); err != nil {
			return fmt.Errorf("mark release %q failed: %w", deployment.ReleaseID, err)
		}
		if err := sqlite.UpdateAppStatus(ctx, deployment.AppID, app.AppStatusFailed, finishedAt); err != nil {
			return fmt.Errorf("mark app %q failed: %w", deployment.AppID, err)
		}
		if err := sqlite.CreateEvent(ctx, app.Event{ID: gitrecv.EventID(deployment.ID, "failed"), AppID: deployment.AppID, Type: "deploy.failed", Message: "Deploy failed after daemon restart for release " + deployment.ReleaseID + ": " + deployment.FailureDetail, CreatedAt: finishedAt}); err != nil {
			return fmt.Errorf("record deployment %q failure: %w", deployment.ID, err)
		}
	}
	return nil
}

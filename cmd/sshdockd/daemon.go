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
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/deployment"
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
	runner := compose.NewDockerRunner(compose.LocalCommandExecutor{})
	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath)
	routing := router.NewService(router.ServiceConfig{Store: sqlite, LocksDir: cfg.LocksDir,
		Router: router.NewCaddyRouter(router.CaddyRouterConfig{ConfigPath: cfg.CaddyConfigPath, MainConfigPath: cfg.CaddyMainConfigPath,
			Executor: router.LocalCommandExecutor{}, AdminAddress: cfg.CaddyAdminAddress, UpstreamHost: "127.0.0.1"}),
	})
	executor := deployment.NewExecutor(deployment.Config{Store: sqlite, Runner: runner, ConfigResolver: configService,
		Routing: routing, Checkout: gitrecv.LocalWorktreeCheckout{}, Output: io.Discard,
	})
	worker := gitrecv.NewQueuedDeploymentWorker(gitrecv.QueuedDeploymentWorkerConfig{Store: sqlite, Executor: executor, LocksDir: cfg.LocksDir})
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
	executor := deployment.NewExecutor(deployment.Config{Store: sqlite, Now: now})
	for _, model := range deployments {
		if err := executor.FailInterrupted(ctx, model); err != nil {
			return fmt.Errorf("recover interrupted deployment %q: %w", model.ID, err)
		}
	}
	return nil
}

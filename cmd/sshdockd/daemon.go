package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/store"
)

func runDaemon(stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	runner, err := hookRunnerFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath)
	service := appmodel.NewService(sqlite, appmodel.WithRecoveryRunner(runner), appmodel.WithWorktreeCheckout(gitrecv.LocalWorktreeCheckout{}), appmodel.WithConfigResolver(configService))
	if err := service.RecoverDeployedApps(ctx); err != nil {
		fmt.Fprintf(stderr, "recover deployed apps: %v\n", err)
		return 1
	}

	<-ctx.Done()
	return 0
}

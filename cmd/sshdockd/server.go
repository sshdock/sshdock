package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/store"
	"github.com/sshdock/sshdock/internal/tui"
)

func runServe(stderr io.Writer) int {
	ctx := context.Background()
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

	sqlite, err := store.OpenSQLite(ctx, cfg.SQLiteDBPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer sqlite.Close()

	runner, err := dashboardRunnerFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath)
	backend := newDashboardBackend(sqlite, cfg, runner, configService)
	handler := tui.NewDashboardHandlerWithConfig(sqlite, runner, configService, backend)
	server := tui.NewSSHServer(tui.SSHServerConfig{
		ListenAddr:         cfg.SSHListenAddr,
		OperatorUser:       cfg.OperatorUser,
		HostKeyPath:        cfg.OperatorHostKeyPath,
		AuthorizedKeysPath: cfg.OperatorAuthorizedKeysPath,
		Handler:            handler,
	})
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintf(stderr, "operator SSH server: %v\n", err)
		return 1
	}
	return 0
}

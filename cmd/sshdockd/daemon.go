package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sshdock/sshdock/internal/config"
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

	<-ctx.Done()
	return 0
}

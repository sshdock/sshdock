package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/iketiunn/rumbase/internal/cli"
	"github.com/iketiunn/rumbase/internal/config"
	"github.com/iketiunn/rumbase/internal/gitrecv"
	"github.com/iketiunn/rumbase/internal/store"
	"github.com/iketiunn/rumbase/internal/version"
)

func main() {
	os.Exit(runWithEnv(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	runner := cli.NewRunner(cli.NewMemoryBackend("server"), version.String())
	return runner.Run(args, stdout, stderr)
}

func runWithEnv(args []string, stdout io.Writer, stderr io.Writer) int {
	if !commandNeedsStore(args) {
		return run(args, stdout, stderr)
	}

	cfg := config.LoadFromEnv()
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
		fmt.Fprintf(stderr, "open store: %v\n", err)
		return 1
	}
	defer sqlite.Close()

	backend := cli.NewStoreBackend(sqlite, cli.StoreBackendConfig{
		NodeID:  cfg.NodeID,
		AppsDir: cfg.AppsDir,
		GitHost: "server",
		RepoSetupper: gitrecv.NewRepoManager(gitrecv.RepoManagerConfig{
			AppsDir:  cfg.AppsDir,
			GitHost:  "server",
			Executor: gitrecv.LocalGitExecutor{},
		}),
	})
	runner := cli.NewRunner(backend, version.String())
	return runner.Run(args, stdout, stderr)
}

func commandNeedsStore(args []string) bool {
	if len(args) == 2 && args[0] == "apps" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "apps" && (args[1] == "create" || args[1] == "info") {
		return true
	}
	if len(args) == 7 && args[0] == "domains" && args[1] == "attach" && args[5] == "--port" {
		_, err := strconv.Atoi(args[6])
		return err == nil
	}

	return false
}

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

func runGitHook(args []string, stdin io.Reader, stderr io.Writer) int {
	flags := flag.NewFlagSet("git-hook", flag.ContinueOnError)
	flags.SetOutput(stderr)
	appName := flags.String("app", "", "app name")
	repoPath := flags.String("repo", "", "bare repository path")
	worktreePath := flags.String("worktree", "", "checkout worktree path")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *appName == "" || *repoPath == "" {
		fmt.Fprintln(stderr, "usage: sshdockd git-hook --app <name> --repo <repo.git> [--worktree <path>]")
		return 2
	}
	if stdin == nil {
		stdin = os.Stdin
	}
	input, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintln(stderr, "deploy: setup failed after remote main update: read hook input:", err)
		return 1
	}
	gitUpdateReported := false
	for _, line := range strings.Split(string(input), "\n") {
		if line == "" {
			continue
		}
		event, parseErr := gitrecv.ParsePostReceiveLine(*appName, *repoPath, line)
		if parseErr != nil {
			fmt.Fprintln(stderr, "deploy: setup failed after remote main update:", parseErr)
			return 1
		}
		if _, writeErr := fmt.Fprintf(stderr, "git: remote main updated to %s\n", event.CommitSHA); writeErr == nil {
			gitUpdateReported = true
		}
	}

	cfg := config.LoadFromEnv()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(stderr, "deploy: setup failed after remote main update:", err)
		return 1
	}
	if *worktreePath == "" {
		*worktreePath = cfg.AppWorktreePath(*appName)
	}
	sqlite, err := store.OpenSQLite(context.Background(), cfg.SQLiteDBPath)
	if err != nil {
		fmt.Fprintln(stderr, "deploy: setup failed after remote main update:", err)
		return 1
	}
	defer sqlite.Close()

	runner, err := hookRunnerFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, "deploy: setup failed after remote main update:", err)
		return 1
	}
	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath, appconfig.WithRecoveryHost(configRecoveryHost(context.Background(), sqlite, cfg)))

	handler := gitrecv.NewPostReceiveHandler(gitrecv.PostReceiveHandlerConfig{
		Store:          sqlite,
		Runner:         runner,
		ConfigResolver: configService,
		Router: router.NewCaddyRouter(router.CaddyRouterConfig{
			ConfigPath:   cfg.CaddyConfigPath,
			Executor:     router.LocalCommandExecutor{},
			AdminAddress: cfg.CaddyAdminAddress,
			UpstreamHost: "127.0.0.1",
		}),
		Checkout:          gitrecv.LocalWorktreeCheckout{},
		Output:            stderr,
		GitUpdateReported: gitUpdateReported,
	})
	if err := handler.Handle(context.Background(), *appName, *repoPath, *worktreePath, bytes.NewReader(input)); err != nil {
		var outputErr *gitrecv.StatusOutputError
		if errors.As(err, &outputErr) {
			fmt.Fprintln(stderr, "deploy: succeeded, but status output failed:", err)
			return 1
		}
		fmt.Fprintln(stderr, "deploy: failed:", err)
		return 1
	}

	return 0
}

func runGitPreReceive(stdin io.Reader, stderr io.Writer) int {
	if stdin == nil {
		stdin = os.Stdin
	}
	if err := gitrecv.ValidatePreReceive(stdin); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runGitReceive(stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	originalCommand := os.Getenv("SSH_ORIGINAL_COMMAND")
	if originalCommand == "" {
		fmt.Fprintln(stderr, "SSH_ORIGINAL_COMMAND is required for git-receive")
		return 2
	}
	if stdin == nil {
		stdin = os.Stdin
	}

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

	service := gitrecv.NewReceivePackService(gitrecv.ReceivePackServiceConfig{
		Store:    sqlite,
		AppsDir:  cfg.AppsDir,
		LocksDir: cfg.LocksDir,
		NodeID:   cfg.NodeID,
		RepoManager: gitrecv.NewRepoManager(gitrecv.RepoManagerConfig{
			AppsDir:  cfg.AppsDir,
			GitHost:  configRecoveryHost(context.Background(), sqlite, cfg),
			Executor: gitrecv.LocalGitExecutor{},
		}),
		ReceivePackRunner: gitrecv.LocalReceivePackRunner{},
	})
	if err := service.Receive(context.Background(), gitrecv.ReceivePackRequest{
		OriginalCommand: originalCommand,
		Stdin:           stdin,
		Stdout:          stdout,
		Stderr:          stderr,
	}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

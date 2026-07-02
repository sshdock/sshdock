package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/iketiunn/rumbase/internal/compose"
	"github.com/iketiunn/rumbase/internal/config"
	"github.com/iketiunn/rumbase/internal/gitrecv"
	"github.com/iketiunn/rumbase/internal/store"
	"github.com/iketiunn/rumbase/internal/tui"
	"github.com/iketiunn/rumbase/internal/version"
)

func main() {
	os.Exit(runWithInput(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	return runWithInput(args, nil, stdout, stderr)
}

func runWithInput(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(stdout, "rhumbased %s\n", version.String())
		return 0
	}
	if len(args) == 0 || (len(args) == 1 && args[0] == "serve") {
		return runServe(stderr)
	}
	if len(args) >= 1 && args[0] == "git-hook" {
		return runGitHook(args[1:], stdin, stderr)
	}
	if len(args) == 1 && args[0] == "git-receive" {
		return runGitReceive(stdin, stdout, stderr)
	}

	fmt.Fprintln(stderr, "usage: rhumbased [serve] | version | git-hook --app <name> --repo <repo.git> [--worktree <path>] | git-receive")
	return 2
}

func runServe(stderr io.Writer) int {
	ctx := context.Background()
	cfg := config.LoadFromEnv()
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

	handler := tui.NewDashboardHandler(sqlite, runner)
	server := tui.NewSSHServer(tui.SSHServerConfig{
		ListenAddr:         cfg.SSHListenAddr,
		DashboardUser:      cfg.DashboardUser,
		HostKeyPath:        cfg.DashboardHostKeyPath,
		AuthorizedKeysPath: cfg.DashboardAuthorizedKeysPath,
		Handler:            handler,
	})
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintf(stderr, "dashboard SSH server: %v\n", err)
		return 1
	}
	return 0
}

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
		fmt.Fprintln(stderr, "usage: rhumbased git-hook --app <name> --repo <repo.git> [--worktree <path>]")
		return 2
	}
	if stdin == nil {
		stdin = os.Stdin
	}

	cfg := config.LoadFromEnv()
	if *worktreePath == "" {
		*worktreePath = cfg.AppWorktreePath(*appName)
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

	handler := gitrecv.NewPostReceiveHandler(gitrecv.PostReceiveHandlerConfig{
		Store:    sqlite,
		Runner:   runner,
		Checkout: gitrecv.LocalWorktreeCheckout{},
	})
	if err := handler.Handle(context.Background(), *appName, *repoPath, *worktreePath, stdin); err != nil {
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
		Store:   sqlite,
		AppsDir: cfg.AppsDir,
		NodeID:  cfg.NodeID,
		RepoManager: gitrecv.NewRepoManager(gitrecv.RepoManagerConfig{
			AppsDir:  cfg.AppsDir,
			GitHost:  cfg.GitHost,
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

func hookRunnerFromEnv() (compose.Runner, error) {
	runner := os.Getenv("RHUMBASE_COMPOSE_RUNNER")
	if runner == "" || runner == "fake" {
		return fakeRunnerFromEnv(), nil
	}
	if runner == "docker" {
		return compose.NewDockerRunner(compose.LocalCommandExecutor{}), nil
	}

	return nil, fmt.Errorf("unsupported RHUMBASE_COMPOSE_RUNNER %q", runner)
}

func dashboardRunnerFromEnv() (compose.Runner, error) {
	runner := os.Getenv("RHUMBASE_COMPOSE_RUNNER")
	if runner == "" || runner == "fake" {
		fake := fakeRunnerFromEnv()
		fake.Services = parseFakeServices(os.Getenv("RHUMBASE_FAKE_COMPOSE_SERVICES"))
		fake.LogOutput = os.Getenv("RHUMBASE_FAKE_COMPOSE_LOGS")
		return fake, nil
	}
	if runner == "docker" {
		return compose.NewDockerRunner(compose.LocalCommandExecutor{}), nil
	}

	return nil, fmt.Errorf("unsupported RHUMBASE_COMPOSE_RUNNER %q", runner)
}

func fakeRunnerFromEnv() *compose.FakeRunner {
	return &compose.FakeRunner{
		DeployErr:  envError("RHUMBASE_FAKE_COMPOSE_DEPLOY_ERROR"),
		RestartErr: envError("RHUMBASE_FAKE_COMPOSE_RESTART_ERROR"),
	}
}

func envError(name string) error {
	value := os.Getenv(name)
	if value == "" {
		return nil
	}
	return errors.New(value)
}

func parseFakeServices(value string) []compose.ServiceStatus {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	services := make([]compose.ServiceStatus, 0, len(parts))
	for _, part := range parts {
		name, state, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok || name == "" {
			continue
		}
		if state == "" {
			state = "unknown"
		}
		services = append(services, compose.ServiceStatus{Name: name, State: state})
	}
	return services
}

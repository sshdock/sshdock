package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/iketiunn/rumbase/internal/cli"
	"github.com/iketiunn/rumbase/internal/compose"
	"github.com/iketiunn/rumbase/internal/config"
	"github.com/iketiunn/rumbase/internal/diagnostics"
	"github.com/iketiunn/rumbase/internal/gitrecv"
	"github.com/iketiunn/rumbase/internal/router"
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
	if len(args) == 1 && args[0] == "diagnostics" {
		return runDiagnostics(stdout)
	}
	if !commandNeedsStore(args) {
		return run(args, stdout, stderr)
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
		fmt.Fprintf(stderr, "open store: %v\n", err)
		return 1
	}
	defer sqlite.Close()

	var recoveryRunner compose.Runner
	if commandNeedsRecoveryRunner(args) {
		recoveryRunner, err = cliRunnerFromEnv()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	backend := cli.NewStoreBackend(sqlite, cli.StoreBackendConfig{
		NodeID:             cfg.NodeID,
		AppsDir:            cfg.AppsDir,
		GitHost:            cfg.GitHost,
		AuthorizedKeysPath: cfg.GitAuthorizedKeysPath,
		GitReceiveCommand:  cfg.GitReceiveCommand,
		Router: router.NewCaddyRouter(router.CaddyRouterConfig{
			ConfigPath:   cfg.CaddyConfigPath,
			Executor:     router.LocalCommandExecutor{},
			AdminAddress: cfg.CaddyAdminAddress,
			UpstreamHost: "127.0.0.1",
		}),
		RepoSetupper: gitrecv.NewRepoManager(gitrecv.RepoManagerConfig{
			AppsDir:  cfg.AppsDir,
			GitHost:  cfg.GitHost,
			Executor: gitrecv.LocalGitExecutor{},
		}),
		RecoveryRunner: recoveryRunner,
	})
	runner := cli.NewRunner(backend, version.String())
	return runner.RunWithInput(args, os.Stdin, stdout, stderr)
}

func runDiagnostics(stdout io.Writer) int {
	report := diagnostics.Run(context.Background(), config.LoadFromEnv(), diagnosticsLocalExecutor{})
	fmt.Fprint(stdout, report.String())
	if !report.OK {
		return 1
	}
	return 0
}

func commandNeedsStore(args []string) bool {
	if len(args) == 2 && args[0] == "apps" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "apps" && (args[1] == "create" || args[1] == "info") {
		return true
	}
	if commandNeedsRecoveryRunner(args) {
		return true
	}
	if len(args) == 7 && args[0] == "domains" && args[1] == "attach" && args[5] == "--port" {
		_, err := strconv.Atoi(args[6])
		return err == nil
	}
	if len(args) == 4 && args[0] == "server" && args[1] == "domain" && args[2] == "set" {
		return true
	}
	if len(args) == 3 && args[0] == "ssh-keys" && args[1] == "add" {
		return true
	}

	return false
}

func commandNeedsRecoveryRunner(args []string) bool {
	if len(args) == 3 && args[0] == "apps" && (args[1] == "restart" || args[1] == "redeploy") {
		return true
	}
	if len(args) == 4 && args[0] == "apps" && (args[1] == "restart" || args[1] == "rollback") {
		return true
	}
	return false
}

func cliRunnerFromEnv() (compose.Runner, error) {
	runner := os.Getenv("RHUMBASE_COMPOSE_RUNNER")
	if runner == "" || runner == "fake" {
		return &compose.FakeRunner{
			DeployErr:  envError("RHUMBASE_FAKE_COMPOSE_DEPLOY_ERROR"),
			RestartErr: envError("RHUMBASE_FAKE_COMPOSE_RESTART_ERROR"),
		}, nil
	}
	if runner == "docker" {
		return compose.NewDockerRunner(compose.LocalCommandExecutor{}), nil
	}

	return nil, fmt.Errorf("unsupported RHUMBASE_COMPOSE_RUNNER %q", runner)
}

func envError(name string) error {
	value := os.Getenv(name)
	if value == "" {
		return nil
	}
	return errors.New(value)
}

type diagnosticsLocalExecutor struct{}

func (diagnosticsLocalExecutor) Run(ctx context.Context, command diagnostics.Command) (string, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w: %s", command.Name, strings.Join(command.Args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

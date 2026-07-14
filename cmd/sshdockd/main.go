package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mattn/go-isatty"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/cli"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/config"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
	"github.com/sshdock/sshdock/internal/tui"
	"github.com/sshdock/sshdock/internal/version"
)

func main() {
	os.Exit(runWithInput(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	return runWithInput(args, nil, stdout, stderr)
}

func runWithInput(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(stdout, "sshdockd %s\n", version.String())
		return 0
	}
	if len(args) == 0 || (len(args) == 1 && args[0] == "serve") {
		return runServe(stderr)
	}
	if len(args) == 1 && args[0] == "daemon" {
		return runDaemon(stderr)
	}
	if len(args) == 1 && args[0] == "dashboard" {
		return runDashboard(stdin, stdout, stderr)
	}
	if len(args) >= 1 && args[0] == "git-hook" {
		return runGitHook(args[1:], stdin, stderr)
	}
	if len(args) == 1 && args[0] == "git-receive" {
		return runGitReceive(stdin, stdout, stderr)
	}

	fmt.Fprintln(stderr, "usage: sshdockd [serve] | daemon | dashboard | version | git-hook --app <name> --repo <repo.git> [--worktree <path>] | git-receive")
	return 2
}

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

	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath, appconfig.WithRecoveryHost(configRecoveryHost(ctx, sqlite, cfg)))
	handler := tui.NewDashboardHandlerWithConfig(sqlite, runner, configService)
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
	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath, appconfig.WithRecoveryHost(configRecoveryHost(ctx, sqlite, cfg)))
	service := appmodel.NewService(sqlite, appmodel.WithRecoveryRunner(runner), appmodel.WithWorktreeCheckout(gitrecv.LocalWorktreeCheckout{}), appmodel.WithConfigResolver(configService))
	if err := service.RecoverDeployedApps(ctx); err != nil {
		fmt.Fprintf(stderr, "recover deployed apps: %v\n", err)
		return 1
	}

	<-ctx.Done()
	return 0
}

func runDashboard(stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
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

	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath, appconfig.WithRecoveryHost(configRecoveryHost(ctx, sqlite, cfg)))
	if originalCommand := strings.TrimSpace(os.Getenv("SSH_ORIGINAL_COMMAND")); originalCommand != "" {
		args, ok := dashboardOriginalCommandArgs(originalCommand)
		if !ok {
			fmt.Fprintln(stderr, "dashboard SSH command supports config commands only")
			return 2
		}
		backend := cli.NewStoreBackend(sqlite, cli.StoreBackendConfig{
			NodeID:        cfg.NodeID,
			AppsDir:       cfg.AppsDir,
			GitHost:       cfg.GitHost,
			ConfigManager: configService,
		})
		runner := cli.NewRunner(backend, version.String())
		return runner.RunWithInput(args, stdin, stdout, stderr)
	}

	runner, err := dashboardRunnerFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	handler := tui.NewDashboardHandlerWithConfig(sqlite, runner, configService)
	snapshot, err := handler.Snapshot(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if stdin == nil {
		stdin = os.Stdin
	}
	if dashboardHasInteractiveTerminal(stdin, stdout) {
		if err := tui.RunInteractiveDashboardWithActions(ctx, snapshot, handler.Snapshot, newDashboardActions(sqlite, cfg, runner, configService), stdin, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}

	if err := tui.RenderDashboardSnapshot(stdout, snapshot); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func dashboardHasInteractiveTerminal(stdin io.Reader, stdout io.Writer) bool {
	input, inputOK := stdin.(*os.File)
	output, outputOK := stdout.(*os.File)
	if !inputOK || !outputOK {
		return false
	}
	return isatty.IsTerminal(input.Fd()) && isatty.IsTerminal(output.Fd())
}

func dashboardOriginalCommandArgs(command string) ([]string, bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 || fields[0] != "config" {
		return nil, false
	}
	return fields, true
}

func configRecoveryHost(ctx context.Context, persistentStore store.Store, cfg config.Config) string {
	if serverConfig, err := persistentStore.GetServerConfig(ctx); err == nil {
		if serverConfig.BaseDomain != "" {
			return domaincfg.ControlHost(serverConfig.BaseDomain)
		}
		if serverConfig.GitHost != "" {
			return serverConfig.GitHost
		}
	}
	return cfg.GitHost
}

type dashboardCLIBackend interface {
	RestartApp(appName string) error
	RestartService(appName string, serviceName string) error
	RedeployApp(appName string) error
	RollbackApp(appName string, releaseID string) error
	AttachDomain(domain cli.Domain) error
	DetachDomain(appName string, domainName string) error
	RemoveApp(appName string) error
}

type dashboardActionBackend struct {
	backend dashboardCLIBackend
}

func newDashboardActions(persistentStore store.Store, cfg config.Config, runner compose.Runner, configService *appconfig.Service) tui.DashboardActions {
	backend := cli.NewStoreBackend(persistentStore, cli.StoreBackendConfig{
		NodeID:                      cfg.NodeID,
		AppsDir:                     cfg.AppsDir,
		GitHost:                     cfg.GitHost,
		AuthorizedKeysPath:          cfg.GitAuthorizedKeysPath,
		GitReceiveCommand:           cfg.GitReceiveCommand,
		DashboardAuthorizedKeysPath: cfg.DashboardAuthorizedKeysPath,
		DashboardCommand:            cfg.DashboardCommand,
		Router: router.NewCaddyRouter(router.CaddyRouterConfig{
			ConfigPath:   cfg.CaddyConfigPath,
			Executor:     router.LocalCommandExecutor{},
			AdminAddress: cfg.CaddyAdminAddress,
			UpstreamHost: "127.0.0.1",
		}),
		RecoveryRunner:   runner,
		RecoveryCheckout: gitrecv.LocalWorktreeCheckout{},
		ConfigManager:    configService,
	})
	return dashboardActionBackend{backend: backend}
}

func (b dashboardActionBackend) RestartApp(appName string) error {
	return b.backend.RestartApp(appName)
}

func (b dashboardActionBackend) RestartService(appName string, serviceName string) error {
	return b.backend.RestartService(appName, serviceName)
}

func (b dashboardActionBackend) RedeployApp(appName string) error {
	return b.backend.RedeployApp(appName)
}

func (b dashboardActionBackend) RollbackApp(appName string, releaseID string) error {
	return b.backend.RollbackApp(appName, releaseID)
}

func (b dashboardActionBackend) AttachDomain(appName string, serviceName string, domainName string, port int) error {
	return b.backend.AttachDomain(cli.Domain{
		AppName:     appName,
		ServiceName: serviceName,
		DomainName:  domainName,
		Port:        port,
	})
}

func (b dashboardActionBackend) DetachDomain(appName string, domainName string) error {
	return b.backend.DetachDomain(appName, domainName)
}

func (b dashboardActionBackend) RemoveApp(appName string) error {
	return b.backend.RemoveApp(appName)
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
		fmt.Fprintln(stderr, "usage: sshdockd git-hook --app <name> --repo <repo.git> [--worktree <path>]")
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
		Store:   sqlite,
		AppsDir: cfg.AppsDir,
		NodeID:  cfg.NodeID,
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

func hookRunnerFromEnv() (compose.Runner, error) {
	runner := os.Getenv("SSHDOCK_COMPOSE_RUNNER")
	if runner == "" || runner == "fake" {
		return fakeRunnerFromEnv(), nil
	}
	if runner == "docker" {
		return compose.NewDockerRunner(compose.LocalCommandExecutor{}), nil
	}

	return nil, fmt.Errorf("unsupported SSHDOCK_COMPOSE_RUNNER %q", runner)
}

func dashboardRunnerFromEnv() (compose.Runner, error) {
	runner := os.Getenv("SSHDOCK_COMPOSE_RUNNER")
	if runner == "" || runner == "fake" {
		fake := fakeRunnerFromEnv()
		fake.Services = parseFakeServices(os.Getenv("SSHDOCK_FAKE_COMPOSE_SERVICES"))
		fake.LogOutput = os.Getenv("SSHDOCK_FAKE_COMPOSE_LOGS")
		return fake, nil
	}
	if runner == "docker" {
		return compose.NewDockerRunner(compose.LocalCommandExecutor{}), nil
	}

	return nil, fmt.Errorf("unsupported SSHDOCK_COMPOSE_RUNNER %q", runner)
}

func fakeRunnerFromEnv() *compose.FakeRunner {
	return &compose.FakeRunner{
		DeployErr:  envError("SSHDOCK_FAKE_COMPOSE_DEPLOY_ERROR"),
		RestartErr: envError("SSHDOCK_FAKE_COMPOSE_RESTART_ERROR"),
		RemoveErr:  envError("SSHDOCK_FAKE_COMPOSE_REMOVE_ERROR"),
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

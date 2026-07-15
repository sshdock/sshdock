package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/cli"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
	"github.com/sshdock/sshdock/internal/tui"
	"github.com/sshdock/sshdock/internal/version"
)

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

	runner, err := dashboardRunnerFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath)
	if originalCommand := strings.TrimSpace(os.Getenv("SSH_ORIGINAL_COMMAND")); originalCommand != "" {
		args, err := operatorOriginalCommandArgs(originalCommand)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		backend := newDashboardBackend(sqlite, cfg, runner, configService)
		runner := cli.NewRunner(backend, version.String())
		if operatorHelpRequested(args) {
			printOperatorHelp(stdout, args)
			return 0
		}
		return runner.RunWithInput(args, stdin, stdout, stderr)
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

func newDashboardBackend(persistentStore store.Store, cfg config.Config, runner compose.Runner, configService *appconfig.Service) *cli.StoreBackend {
	return cli.NewStoreBackend(persistentStore, cli.StoreBackendConfig{
		NodeID:                     cfg.NodeID,
		AppsDir:                    cfg.AppsDir,
		GitHost:                    cfg.GitHost,
		AuthorizedKeysPath:         cfg.GitAuthorizedKeysPath,
		GitReceiveCommand:          cfg.GitReceiveCommand,
		OperatorAuthorizedKeysPath: cfg.OperatorAuthorizedKeysPath,
		OperatorCommand:            cfg.OperatorCommand,
		Router: router.NewCaddyRouter(router.CaddyRouterConfig{
			ConfigPath:   cfg.CaddyConfigPath,
			Executor:     router.LocalCommandExecutor{},
			AdminAddress: cfg.CaddyAdminAddress,
			UpstreamHost: "127.0.0.1",
		}),
		RecoveryRunner:      runner,
		RecoveryCheckout:    gitrecv.LocalWorktreeCheckout{},
		CurrentMainResolver: gitrecv.LocalCurrentMainResolver{},
		ConfigManager:       configService,
	})
}

func newDashboardActions(persistentStore store.Store, cfg config.Config, runner compose.Runner, configService *appconfig.Service) tui.DashboardActions {
	return dashboardActionBackend{backend: newDashboardBackend(persistentStore, cfg, runner, configService)}
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

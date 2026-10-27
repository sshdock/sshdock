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
	originalCommand := strings.TrimSpace(os.Getenv("SSH_ORIGINAL_COMMAND"))
	var args []string
	var commandErr error
	if originalCommand != "" {
		args, commandErr = operatorOriginalCommandArgs(originalCommand)
	}
	normalized, jsonOutput, outputErr := cli.ParseOutputArgs(args)
	fail := func(err error, code int) int {
		if jsonOutput {
			if writeErr := cli.WriteJSON(stdout, cli.JSONCommandName(args), nil, err); writeErr != nil {
				fmt.Fprintln(stderr, writeErr)
				return 1
			}
		} else {
			fmt.Fprintln(stderr, err)
		}
		return code
	}
	if outputErr != nil {
		return fail(outputErr, 2)
	}
	if commandErr != nil {
		return fail(cli.JSONError("invalid_arguments", commandErr.Error()), 2)
	}
	ctx := context.Background()
	cfg := config.LoadFromEnv()
	if err := cfg.Validate(); err != nil {
		return fail(err, 1)
	}
	readOnlyCheck := len(normalized) == 3 && normalized[0] == "apps" && normalized[1] == "check"
	if !readOnlyCheck {
		if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
			return fail(fmt.Errorf("create data dir: %w", err), 1)
		}
		if err := os.MkdirAll(filepath.Dir(cfg.SQLiteDBPath), 0o755); err != nil {
			return fail(fmt.Errorf("create database dir: %w", err), 1)
		}
	}

	var sqlite *store.SQLiteStore
	var err error
	if readOnlyCheck {
		sqlite, err = store.OpenSQLiteReadOnly(ctx, cfg.SQLiteDBPath)
	} else {
		sqlite, err = store.OpenSQLite(ctx, cfg.SQLiteDBPath)
	}
	if err != nil {
		return fail(err, 1)
	}
	defer sqlite.Close()

	composeRunner := compose.NewDockerRunner(compose.LocalCommandExecutor{})

	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath)
	backend := newDashboardBackend(sqlite, cfg, composeRunner, configService)
	if originalCommand != "" {
		cliRunner := cli.NewRunner(backend, version.String()).WithInteractiveTerminal(dashboardHasInteractiveTerminal(stdin, stdout))
		if operatorHelpRequested(args) {
			printOperatorHelp(stdout, args)
			return 0
		}
		return cliRunner.RunWithInput(args, stdin, stdout, stderr)
	}

	handler := tui.NewDashboardHandlerWithConfig(sqlite, composeRunner, configService, backend)
	if stdin == nil {
		stdin = os.Stdin
	}
	if dashboardHasInteractiveTerminal(stdin, stdout) {
		snapshot, err := handler.Snapshot(ctx)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := tui.RunInteractiveDashboardWithActions(ctx, snapshot, handler.Snapshot, handler.Logs, dashboardActionBackend{backend: backend}, stdin, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}

	if err := handler.Render(ctx, stdout); err != nil {
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
	StartApp(appName string) error
	StopApp(appName string) error
	RestartApp(appName string) error
	RestartService(appName string, serviceName string) error
	RedeployApp(appName string) error
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
		LocksDir:                   cfg.LocksDir,
		GitHost:                    cfg.GitHost,
		AuthorizedKeysPath:         cfg.GitAuthorizedKeysPath,
		GitReceiveCommand:          cfg.GitReceiveCommand,
		OperatorAuthorizedKeysPath: cfg.OperatorAuthorizedKeysPath,
		OperatorCommand:            cfg.OperatorCommand,
		Router: router.NewCaddyRouter(router.CaddyRouterConfig{
			ConfigPath:     cfg.CaddyConfigPath,
			MainConfigPath: cfg.CaddyMainConfigPath,
			Executor:       router.LocalCommandExecutor{},
			AdminAddress:   cfg.CaddyAdminAddress,
			UpstreamHost:   "127.0.0.1",
		}),
		RecoveryRunner:      runner,
		CurrentMainResolver: gitrecv.LocalCurrentMainResolver{},
		ConfigManager:       configService,
	})
}

func (b dashboardActionBackend) StartApp(appName string) error {
	return b.backend.StartApp(appName)
}

func (b dashboardActionBackend) StopApp(appName string) error {
	return b.backend.StopApp(appName)
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

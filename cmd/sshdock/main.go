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
	"time"

	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/backup"
	"github.com/sshdock/sshdock/internal/cli"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/diagnostics"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
	"github.com/sshdock/sshdock/internal/version"
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
	if len(args) >= 1 && args[0] == "backup" {
		return runBackup(args[1:], stdout, stderr)
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

	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath, appconfig.WithRecoveryHost(configRecoveryHost(context.Background(), sqlite, cfg)))

	var recoveryRunner compose.Runner
	if commandNeedsRecoveryRunner(args) {
		recoveryRunner, err = cliRunnerFromEnv()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	backend := cli.NewStoreBackend(sqlite, cli.StoreBackendConfig{
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
		RepoSetupper: gitrecv.NewRepoManager(gitrecv.RepoManagerConfig{
			AppsDir:  cfg.AppsDir,
			GitHost:  cfg.GitHost,
			Executor: gitrecv.LocalGitExecutor{},
		}),
		RecoveryRunner:      recoveryRunner,
		RecoveryCheckout:    gitrecv.LocalWorktreeCheckout{},
		CurrentMainResolver: gitrecv.LocalCurrentMainResolver{},
		ConfigManager:       configService,
	})
	runner := cli.NewRunner(backend, version.String())
	return runner.RunWithInput(args, os.Stdin, stdout, stderr)
}

func runBackup(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && isBackupHelpArg(args[0])) {
		printBackupUsage(stdout)
		return 0
	}

	switch args[0] {
	case "create":
		output, includeVolumes, ok := parseBackupCreateArgs(args[1:])
		if !ok {
			printBackupInvalidUsage(stderr)
			return 2
		}
		cfg := config.LoadFromEnv()
		result, err := backup.Create(context.Background(), backup.CreateRequest{
			Config:         cfg,
			Destination:    output,
			IncludeVolumes: includeVolumes,
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "created backup %s\n", result.Path)
		fmt.Fprintf(stdout, "files: %d\n", result.FileCount)
		fmt.Fprintf(stdout, "Docker volume inventory: %d\n", result.VolumeCount)
		return 0
	case "inspect":
		if len(args) != 2 || isBackupHelpArg(args[1]) {
			printBackupInvalidUsage(stderr)
			return 2
		}
		inspection, err := backup.Inspect(context.Background(), args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "archive: %s\n", inspection.Path)
		fmt.Fprintf(stdout, "format: %s\n", inspection.Manifest.FormatVersion)
		fmt.Fprintf(stdout, "created: %s\n", inspection.Manifest.CreatedAt.Format(time.RFC3339))
		fmt.Fprintf(stdout, "files: %d\n", inspection.FileCount)
		fmt.Fprintf(stdout, "Docker volumes: %d\n", inspection.VolumeCount)
		for _, volume := range inspection.Volumes {
			fmt.Fprintf(stdout, "  %s", volume.Name)
			if volume.Driver != "" {
				fmt.Fprintf(stdout, "\t%s", volume.Driver)
			}
			fmt.Fprintln(stdout)
		}
		if len(inspection.Manifest.RestoreGuardrails) > 0 {
			fmt.Fprintln(stdout, "restore guardrails:")
			for _, guardrail := range inspection.Manifest.RestoreGuardrails {
				fmt.Fprintf(stdout, "  - %s\n", guardrail)
			}
		}
		return 0
	case "restore":
		if len(args) != 2 || isBackupHelpArg(args[1]) {
			printBackupInvalidUsage(stderr)
			return 2
		}
		cfg := config.LoadFromEnv()
		if err := backup.Restore(context.Background(), backup.RestoreRequest{Config: cfg, ArchivePath: args[1]}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "restored backup %s\n", args[1])
		fmt.Fprintf(stdout, "data dir: %s\n", cfg.DataDir)
		fmt.Fprintln(stdout, "run sudo sshdock diagnostics")
		return 0
	default:
		printBackupInvalidUsage(stderr)
		return 2
	}
}

func parseBackupCreateArgs(args []string) (string, bool, bool) {
	var output string
	includeVolumes := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--include-volumes":
			includeVolumes = true
		case "-o", "--output":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", false, false
			}
			output = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "--output=") {
				output = strings.TrimPrefix(args[i], "--output=")
				if strings.TrimSpace(output) == "" {
					return "", false, false
				}
				continue
			}
			return "", false, false
		}
	}
	return output, includeVolumes, true
}

func isBackupHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

func printBackupUsage(stdout io.Writer) {
	fmt.Fprint(stdout, `Backup commands create, inspect, and restore SSHDock state archives.

Usage:
  sshdock backup create [--output <archive>]
  sshdock backup inspect <archive>
  sshdock backup restore <archive>

Notes:
  Backup archives include Docker volume inventory only.
  --include-volumes is rejected until volume content backup has a safe implementation.
`)
}

func printBackupInvalidUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "invalid backup command or arguments")
	fmt.Fprintln(stderr, `Run "sshdock help backup" for usage.`)
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
	if commandIsHelpRequest(args) {
		return false
	}
	if len(args) >= 1 && args[0] == "config" {
		return true
	}
	if len(args) == 2 && args[0] == "apps" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "apps" && (args[1] == "create" || args[1] == "info" || args[1] == "health") {
		return true
	}
	if len(args) >= 3 && len(args) <= 4 && args[0] == "apps" && args[1] == "remove" {
		return true
	}
	if commandNeedsRecoveryRunner(args) {
		return true
	}
	if len(args) == 3 && args[0] == "releases" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "deployments" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "events" && args[1] == "list" {
		return true
	}
	if len(args) == 7 && args[0] == "domains" && args[1] == "attach" && args[5] == "--port" {
		_, err := strconv.Atoi(args[6])
		return err == nil
	}
	if len(args) == 3 && args[0] == "domains" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "domains" && args[1] == "check" {
		return true
	}
	if len(args) == 4 && args[0] == "domains" && args[1] == "detach" {
		return true
	}
	if len(args) == 4 && args[0] == "server" && args[1] == "domain" && args[2] == "set" {
		return true
	}
	if len(args) == 2 && args[0] == "ssh-keys" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "ssh-keys" && args[1] == "add" {
		return true
	}
	if len(args) == 3 && args[0] == "ssh-keys" && args[1] == "remove" {
		return true
	}

	return false
}

func commandIsHelpRequest(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if len(args) == 1 {
		return args[0] == "help" || args[0] == "-h" || args[0] == "--help"
	}
	if args[0] == "help" {
		return true
	}
	return len(args) == 2 && (args[1] == "-h" || args[1] == "--help")
}

func commandNeedsRecoveryRunner(args []string) bool {
	if len(args) >= 2 && args[0] == "logs" {
		return true
	}
	if len(args) == 3 && args[0] == "apps" && args[1] == "health" {
		return true
	}
	if len(args) == 3 && args[0] == "apps" && (args[1] == "restart" || args[1] == "redeploy") {
		return true
	}
	if len(args) == 4 && args[0] == "apps" && (args[1] == "restart" || args[1] == "rollback") {
		return true
	}
	if len(args) >= 3 && len(args) <= 4 && args[0] == "apps" && args[1] == "remove" {
		return true
	}
	return false
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

func cliRunnerFromEnv() (compose.Runner, error) {
	runner := os.Getenv("SSHDOCK_COMPOSE_RUNNER")
	if runner == "" || runner == "docker" {
		return compose.NewDockerRunner(compose.LocalCommandExecutor{}), nil
	}
	if runner == "fake" {
		return &compose.FakeRunner{
			DeployErr:  envError("SSHDOCK_FAKE_COMPOSE_DEPLOY_ERROR"),
			RestartErr: envError("SSHDOCK_FAKE_COMPOSE_RESTART_ERROR"),
			RemoveErr:  envError("SSHDOCK_FAKE_COMPOSE_REMOVE_ERROR"),
			Services:   parseFakeServices(os.Getenv("SSHDOCK_FAKE_COMPOSE_SERVICES")),
			LogOutput:  os.Getenv("SSHDOCK_FAKE_COMPOSE_LOGS"),
		}, nil
	}

	return nil, fmt.Errorf("unsupported SSHDOCK_COMPOSE_RUNNER %q", runner)
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
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		services = append(services, compose.ServiceStatus{
			Name:  strings.TrimSpace(name),
			State: strings.TrimSpace(state),
		})
	}
	return services
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

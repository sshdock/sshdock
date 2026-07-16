package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/backup"
	"github.com/sshdock/sshdock/internal/cli"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/diagnostics"
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

	configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath)

	var recoveryRunner compose.Runner
	if commandNeedsRecoveryRunner(args) {
		recoveryRunner, err = cliRunnerFromEnv()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	backend := cli.NewStoreBackend(sqlite, cli.StoreBackendConfig{
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

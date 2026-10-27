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
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
	"github.com/sshdock/sshdock/internal/version"
)

func main() {
	os.Exit(runWithEnv(os.Args[1:], os.Stdout, os.Stderr))
}

func runWithEnv(args []string, stdout io.Writer, stderr io.Writer) int {
	originalArgs := args
	args, jsonOutput, parseErr := cli.ParseOutputArgs(args)
	fail := func(err error, code int) int {
		if jsonOutput {
			if writeErr := cli.WriteJSON(stdout, cli.JSONCommandName(originalArgs), nil, err); writeErr != nil {
				fmt.Fprintln(stderr, writeErr)
				return 1
			}
		} else {
			fmt.Fprintln(stderr, err)
		}
		return code
	}
	if parseErr != nil {
		return fail(parseErr, 2)
	}
	if len(args) >= 1 && args[0] == "check" {
		return runLocalPreflight(args[1:], jsonOutput, stdout, stderr)
	}
	if len(args) == 1 && args[0] == "diagnostics" {
		return runDiagnostics(stdout, stderr, jsonOutput)
	}
	if len(args) >= 1 && args[0] == "backup" {
		return runBackup(args[1:], stdout, stderr)
	}
	var sqlite *store.SQLiteStore
	defer func() {
		if sqlite != nil {
			_ = sqlite.Close()
		}
	}()
	runner := cli.NewRunnerWithBackendFactory(func() (cli.Backend, error) {
		cfg := config.LoadFromEnv()
		if args[0] == "apps" && args[1] == "create" {
			if err := domaincfg.ValidateAppName(args[2]); err != nil {
				return nil, gitrecv.WithAppNameGuidance(err, readOnlyAppCreationHost(cfg))
			}
		}
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		readOnlyCheck := args[0] == "apps" && args[1] == "check"
		if !readOnlyCheck {
			if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
				return nil, fmt.Errorf("create data dir: %w", err)
			}
			if err := os.MkdirAll(filepath.Dir(cfg.SQLiteDBPath), 0o755); err != nil {
				return nil, fmt.Errorf("create database dir: %w", err)
			}
		}

		var err error
		if readOnlyCheck {
			sqlite, err = store.OpenSQLiteReadOnly(context.Background(), cfg.SQLiteDBPath)
		} else {
			sqlite, err = store.OpenSQLite(context.Background(), cfg.SQLiteDBPath)
		}
		if err != nil {
			return nil, fmt.Errorf("open store: %w", err)
		}

		configService := appconfig.NewService(sqlite, cfg.ConfigKeyPath)

		return cli.NewStoreBackend(sqlite, cli.StoreBackendConfig{
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
			RepoSetupper: gitrecv.NewRepoManager(gitrecv.RepoManagerConfig{
				AppsDir:   cfg.AppsDir,
				GitHost:   cfg.GitHost,
				Executor:  gitrecv.LocalGitExecutor{},
				OwnerUser: cfg.OperatorUser,
			}),
			RecoveryRunner:      compose.NewDockerRunner(compose.LocalCommandExecutor{}),
			CurrentMainResolver: gitrecv.LocalCurrentMainResolver{},
			ConfigManager:       configService,
		}), nil
	}, version.String())
	return runner.RunWithInput(originalArgs, os.Stdin, stdout, stderr)
}

func readOnlyAppCreationHost(cfg config.Config) string {
	ctx := context.Background()
	sqlite, err := store.OpenSQLiteReadOnly(ctx, cfg.SQLiteDBPath)
	if err != nil {
		return cfg.GitHost
	}
	defer sqlite.Close()
	serverConfig, err := sqlite.GetServerConfig(ctx)
	if err == nil {
		if serverConfig.BaseDomain != "" {
			return domaincfg.ControlHost(serverConfig.BaseDomain)
		}
		if serverConfig.GitHost != "" {
			return serverConfig.GitHost
		}
	}
	return cfg.GitHost
}

func runBackup(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && isBackupHelpArg(args[0])) {
		printBackupUsage(stdout)
		return 0
	}

	switch args[0] {
	case "volume":
		return runVolumeBackup(args[1:], stdout, stderr)
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
	fmt.Fprint(stdout, `Backup commands manage host-state and separate manual volume archives.

Usage:
  sshdock backup create [--output <archive>]
  sshdock backup inspect <archive>
  sshdock backup restore <archive>
  sshdock backup volume export <volume> --output <archive> --quiesced
  sshdock backup volume inspect <archive>
  sshdock backup volume restore <archive> --name <new-volume>

Notes:
  Host-state archives include Docker volume inventory only; --include-volumes remains unsupported.
  Volume export requires stopped writers and an explicit --quiesced acknowledgement.
  Volume restore creates a new volume and refuses existing targets.
  Archives are unencrypted and may contain secrets; protect and encrypt them yourself.
`)
}

func printBackupInvalidUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "invalid backup command or arguments")
	fmt.Fprintln(stderr, `Run "sshdock help backup" for usage.`)
}

func runDiagnostics(stdout io.Writer, stderr io.Writer, jsonOutput bool) int {
	report := diagnostics.Run(context.Background(), config.LoadFromEnv(), diagnosticsLocalExecutor{})
	if jsonOutput {
		if err := cli.WriteJSON(stdout, "diagnostics", report, nil); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		fmt.Fprint(stdout, report.String())
	}
	if !report.OK {
		return 1
	}
	return 0
}

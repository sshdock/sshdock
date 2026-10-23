package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sshdock/sshdock/internal/backup"
)

func runVolumeBackup(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || len(args) == 1 && isBackupHelpArg(args[0]) {
		printVolumeBackupUsage(stdout)
		return 0
	}
	if len(args) == 2 && isBackupHelpArg(args[1]) {
		printVolumeBackupUsage(stdout)
		return 0
	}
	if len(args) < 2 || strings.HasPrefix(args[1], "-") || args[1] == "" {
		printVolumeBackupUsage(stderr)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var inspection backup.VolumeInspection
	var err error
	switch args[0] {
	case "export":
		flags := flag.NewFlagSet("backup volume export", flag.ContinueOnError)
		flags.SetOutput(stderr)
		flags.Usage = func() {}
		output := flags.String("output", "", "destination archive (must not exist)")
		quiesced := flags.Bool("quiesced", false, "acknowledge that all writers are stopped")
		if err := flags.Parse(args[2:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				printVolumeBackupUsage(stdout)
				return 0
			}
			return 2
		}
		if flags.NArg() != 0 || strings.TrimSpace(*output) == "" || !*quiesced {
			fmt.Fprintln(stderr, "export requires --output <new-archive> and --quiesced after stopping all writers")
			return 2
		}
		inspection, err = backup.ExportVolume(ctx, backup.VolumeExportRequest{VolumeName: args[1], ArchivePath: *output, Quiesced: *quiesced})
		if err == nil {
			fmt.Fprintf(stdout, "exported volume %s\n", args[1])
		}
	case "inspect":
		if len(args) != 2 {
			printVolumeBackupUsage(stderr)
			return 2
		}
		inspection, err = backup.InspectVolume(ctx, args[1])
	case "restore":
		flags := flag.NewFlagSet("backup volume restore", flag.ContinueOnError)
		flags.SetOutput(stderr)
		flags.Usage = func() {}
		name := flags.String("name", "", "new destination volume (must not exist)")
		if err := flags.Parse(args[2:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				printVolumeBackupUsage(stdout)
				return 0
			}
			return 2
		}
		if flags.NArg() != 0 || strings.TrimSpace(*name) == "" {
			fmt.Fprintln(stderr, "restore requires --name <new-volume>; existing volumes cannot be overwritten")
			return 2
		}
		inspection, err = backup.RestoreVolume(ctx, backup.VolumeRestoreRequest{VolumeName: *name, ArchivePath: args[1]})
		if err == nil {
			fmt.Fprintf(stdout, "restored new volume %s\n", *name)
		}
	default:
		printVolumeBackupUsage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	printVolumeInspection(stdout, inspection)
	return 0
}

func printVolumeInspection(stdout io.Writer, inspection backup.VolumeInspection) {
	fmt.Fprintf(stdout, "archive: %s\nformat: %s\nsource volume: %s\ncreated: %s\nfiles: %d\ndirectories: %d\nbytes: %d\n",
		inspection.Path, inspection.Manifest.FormatVersion, inspection.Manifest.SourceVolume, inspection.Manifest.CreatedAt.Format(time.RFC3339), inspection.FileCount, inspection.DirectoryCount, inspection.TotalBytes)
	fmt.Fprintln(stdout, "scope: regular files/directories, numeric UID/GID and basic modes; no complete filesystem metadata guarantee")
	fmt.Fprintln(stdout, "unencrypted archive: may contain secrets; use private permissions, your own encryption and protected storage")
}

func printVolumeBackupUsage(stdout io.Writer) {
	fmt.Fprint(stdout, `Manual named-volume data commands (local host administration):
  sshdock backup volume export <volume> --output <archive> --quiesced
  sshdock backup volume inspect <archive>
  sshdock backup volume restore <archive> --name <new-volume>

Keep all writers stopped for the export window, including other apps and host processes.
Only ordinary local Docker volumes without driver options are supported.
Archives preserve regular files/directories, numeric UID/GID and basic modes.
This is not an application-consistent live database snapshot or a full filesystem backup.
Archives are unencrypted and may contain secrets. Existing archives/volumes are refused.
Use PostgreSQL-native dump/restore for a running PostgreSQL database.
`)
}

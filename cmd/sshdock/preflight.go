package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/sshdock/sshdock/internal/cli"
	"github.com/sshdock/sshdock/internal/compose"
)

func runLocalPreflight(args []string, jsonOutput bool, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(stdout, "Usage: sshdock check [directory] [--json]")
		return 0
	}
	if len(args) > 1 || len(args) == 1 && (args[0] == "" || strings.HasPrefix(args[0], "-")) {
		fmt.Fprintln(stderr, "invalid check arguments; run sshdock help check")
		return 2
	}
	directory := "."
	if len(args) == 1 {
		directory = args[0]
	}
	projectDir, err := filepath.Abs(directory)
	var report compose.PreflightReport
	if err == nil {
		runner := compose.NewDockerRunner(compose.LocalCommandExecutor{})
		report, err = runner.Preflight(context.Background(), compose.PreflightRequest{AppName: "preflight", ProjectDir: projectDir, Scope: "local"})
	}
	if jsonOutput {
		if writeErr := cli.WriteJSON(stdout, "check", report, err); writeErr != nil {
			fmt.Fprintln(stderr, writeErr)
			return 1
		}
	} else if err != nil {
		fmt.Fprintln(stderr, err)
	} else if writeErr := cli.PrintPreflight(stdout, report); writeErr != nil {
		fmt.Fprintln(stderr, writeErr)
		return 1
	}
	if err != nil || !report.OK {
		return 1
	}
	return 0
}

package main

import (
	"fmt"
	"io"
	"os"

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
	if len(args) == 1 && args[0] == "git-pre-receive" {
		return runGitPreReceive(stdin, stderr)
	}
	if len(args) == 1 && args[0] == "git-receive" {
		return runGitReceive(stdin, stdout, stderr)
	}

	fmt.Fprintln(stderr, "usage: sshdockd [serve] | daemon | dashboard | version | git-pre-receive | git-hook --app <name> --repo <repo.git> [--worktree <path>] | git-receive")
	return 2
}

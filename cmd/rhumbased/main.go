package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/iketiunn/rumbase/internal/compose"
	"github.com/iketiunn/rumbase/internal/config"
	"github.com/iketiunn/rumbase/internal/gitrecv"
	"github.com/iketiunn/rumbase/internal/store"
	"github.com/iketiunn/rumbase/internal/version"
)

func main() {
	os.Exit(runWithInput(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	return runWithInput(args, nil, stdout, stderr)
}

func runWithInput(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(stdout, "rhumbased %s\n", version.String())
		return 0
	}
	if len(args) >= 1 && args[0] == "git-hook" {
		return runGitHook(args[1:], stdin, stderr)
	}

	fmt.Fprintln(stderr, "usage: rhumbased version | git-hook --app <name> --repo <repo.git> [--worktree <path>]")
	return 2
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
		fmt.Fprintln(stderr, "usage: rhumbased git-hook --app <name> --repo <repo.git> [--worktree <path>]")
		return 2
	}
	if stdin == nil {
		stdin = os.Stdin
	}

	cfg := config.LoadFromEnv()
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

	handler := gitrecv.NewPostReceiveHandler(gitrecv.PostReceiveHandlerConfig{
		Store:    sqlite,
		Runner:   runner,
		Checkout: gitrecv.LocalWorktreeCheckout{},
	})
	if err := handler.Handle(context.Background(), *appName, *repoPath, *worktreePath, stdin); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func hookRunnerFromEnv() (compose.Runner, error) {
	runner := os.Getenv("RHUMBASE_COMPOSE_RUNNER")
	if runner == "" || runner == "fake" {
		return &compose.FakeRunner{}, nil
	}

	return nil, fmt.Errorf("unsupported RHUMBASE_COMPOSE_RUNNER %q", runner)
}

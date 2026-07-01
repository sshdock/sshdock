package gitrecv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type LocalGitExecutor struct{}

func (LocalGitExecutor) Run(ctx context.Context, command GitCommand) error {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s failed: %w\n%s", command.Name, strings.Join(command.Args, " "), err, output)
	}

	return nil
}

type LocalWorktreeCheckout struct{}

func (LocalWorktreeCheckout) Checkout(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error {
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		return err
	}

	return LocalGitExecutor{}.Run(ctx, GitCommand{
		Name: "git",
		Args: []string{"--git-dir", repoPath, "--work-tree", worktreePath, "checkout", "-f", commitSHA},
	})
}

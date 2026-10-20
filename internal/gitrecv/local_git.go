package gitrecv

import (
	"context"
	"errors"
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

type LocalCurrentMainResolver struct{}

var ErrMainNotFound = errors.New("remote main does not exist")

func (LocalCurrentMainResolver) ResolveCurrentMain(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "--git-dir", repoPath, "rev-parse", "--verify", "--quiet", mainRef+"^{commit}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && len(output) == 0 {
			refOutput, refErr := exec.CommandContext(ctx, "git", "--git-dir", repoPath, "rev-parse", "--verify", "--quiet", mainRef).CombinedOutput()
			if errors.As(refErr, &exitErr) && exitErr.ExitCode() == 1 && len(refOutput) == 0 {
				return "", fmt.Errorf("resolve %s in %s: %w", mainRef, repoPath, ErrMainNotFound)
			}
			if refErr != nil {
				return "", fmt.Errorf("resolve %s in %s: %w\n%s", mainRef, repoPath, refErr, refOutput)
			}
			return "", fmt.Errorf("resolve %s in %s: ref %s is not a valid commit: %w", mainRef, repoPath, strings.TrimSpace(string(refOutput)), err)
		}
		return "", fmt.Errorf("resolve %s in %s: %w\n%s", mainRef, repoPath, err, output)
	}
	commitSHA := strings.TrimSpace(string(output))
	if commitSHA == "" {
		return "", fmt.Errorf("resolve %s in %s: git returned an empty commit", mainRef, repoPath)
	}
	return commitSHA, nil
}

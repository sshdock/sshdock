//go:build e2e

package gitrecv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalCurrentMainResolverReportsMissingMain(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "repo.git")
	runGitTestCommand(t, "", "init", "--bare", repoPath)

	_, err := (LocalCurrentMainResolver{}).ResolveCurrentMain(context.Background(), repoPath)

	if !errors.Is(err, ErrMainNotFound) {
		t.Fatalf("ResolveCurrentMain error = %v, want ErrMainNotFound", err)
	}
}

func TestLocalCurrentMainResolverPreservesMissingRepositoryError(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "missing.git")

	_, err := (LocalCurrentMainResolver{}).ResolveCurrentMain(context.Background(), repoPath)

	if err == nil || errors.Is(err, ErrMainNotFound) {
		t.Fatalf("ResolveCurrentMain error = %v, want repository error", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 128 {
		t.Fatalf("ResolveCurrentMain error = %v, want Git repository failure exit 128", err)
	}
}

func TestLocalCurrentMainResolverPreservesDamagedMainError(t *testing.T) {
	for _, tt := range []struct {
		name, ref string
	}{
		{name: "malformed ref", ref: "garbage\n"},
		{name: "missing object", ref: strings.Repeat("f", 40) + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repoPath := filepath.Join(t.TempDir(), "repo.git")
			runGitTestCommand(t, "", "init", "--bare", repoPath)
			if err := os.WriteFile(filepath.Join(repoPath, "refs", "heads", "main"), []byte(tt.ref), 0o644); err != nil {
				t.Fatalf("write damaged ref: %v", err)
			}

			_, err := (LocalCurrentMainResolver{}).ResolveCurrentMain(context.Background(), repoPath)

			if err == nil || errors.Is(err, ErrMainNotFound) {
				t.Fatalf("ResolveCurrentMain error = %v, want damaged main error", err)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("ResolveCurrentMain error = %v, want Git resolution failure exit 1", err)
			}
		})
	}
}

func TestLocalCurrentMainResolverResolvesBareRepoMainCommit(t *testing.T) {
	// Given
	ctx := context.Background()
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	repoPath := filepath.Join(t.TempDir(), "repo.git")
	runGitTestCommand(t, "", "init", "-b", "main", worktreePath)
	runGitTestCommand(t, worktreePath, "config", "user.name", "SSHDock Test")
	runGitTestCommand(t, worktreePath, "config", "user.email", "test@sshdock.local")
	if err := os.WriteFile(filepath.Join(worktreePath, "README.md"), []byte("current main\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runGitTestCommand(t, worktreePath, "add", "README.md")
	runGitTestCommand(t, worktreePath, "commit", "-m", "Initial commit")
	want := gitTestOutput(t, worktreePath, "rev-parse", "HEAD")
	runGitTestCommand(t, "", "clone", "--bare", worktreePath, repoPath)

	// When
	got, err := (LocalCurrentMainResolver{}).ResolveCurrentMain(ctx, repoPath)

	// Then
	if err != nil {
		t.Fatalf("ResolveCurrentMain: %v", err)
	}
	if got != want {
		t.Fatalf("commit = %q, want %q", got, want)
	}
}

func runGitTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitTestOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

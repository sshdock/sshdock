package gitrecv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRepoManagerSetupBareRepo(t *testing.T) {
	ctx := context.Background()
	appsDir := t.TempDir()
	executor := &recordingGitExecutor{}
	manager := NewRepoManager(RepoManagerConfig{
		AppsDir:  appsDir,
		GitHost:  "example.com",
		Executor: executor,
	})

	repo, err := manager.SetupBareRepo(ctx, "my-app")
	if err != nil {
		t.Fatalf("SetupBareRepo: %v", err)
	}

	wantRepoPath := filepath.Join(appsDir, "my-app", "repo.git")
	if repo.Path != wantRepoPath {
		t.Fatalf("repo path = %q, want %q", repo.Path, wantRepoPath)
	}
	if repo.RemoteURL != "git@example.com:my-app" {
		t.Fatalf("remote URL = %q", repo.RemoteURL)
	}

	wantCommands := []GitCommand{
		{Name: "git", Args: []string{"init", "--bare", wantRepoPath}},
	}
	if !reflect.DeepEqual(executor.Commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, wantCommands)
	}

	hookPath := filepath.Join(wantRepoPath, "hooks", "post-receive")
	hook, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if !strings.Contains(string(hook), "my-app") || !strings.Contains(string(hook), wantRepoPath) {
		t.Fatalf("hook does not include app name and repo path:\n%s", hook)
	}

	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("stat hook: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("hook permissions = %v, want 0755", info.Mode().Perm())
	}
}

func TestRepoManagerReturnsExecutorError(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("git failed")
	manager := NewRepoManager(RepoManagerConfig{
		AppsDir:  t.TempDir(),
		GitHost:  "example.com",
		Executor: &recordingGitExecutor{Err: failure},
	})

	_, err := manager.SetupBareRepo(ctx, "my-app")
	if !errors.Is(err, failure) {
		t.Fatalf("SetupBareRepo error = %v, want %v", err, failure)
	}
}

func TestRepoManagerUsesDefaultRemoteHost(t *testing.T) {
	manager := NewRepoManager(RepoManagerConfig{AppsDir: t.TempDir(), Executor: &recordingGitExecutor{}})

	if got := manager.RemoteURL("my-app"); got != "git@server:my-app" {
		t.Fatalf("RemoteURL = %q", got)
	}
}

type recordingGitExecutor struct {
	Commands []GitCommand
	Err      error
}

func (r *recordingGitExecutor) Run(_ context.Context, command GitCommand) error {
	r.Commands = append(r.Commands, command)
	return r.Err
}

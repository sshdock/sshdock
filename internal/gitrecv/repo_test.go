package gitrecv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	if repo.RemoteURL != "git@example.com:my-app.git" {
		t.Fatalf("remote URL = %q", repo.RemoteURL)
	}

	wantCommands := []GitCommand{
		{Name: "git", Args: []string{"init", "--bare", wantRepoPath}},
	}
	if !reflect.DeepEqual(executor.Commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, wantCommands)
	}

	assertReceiveHook(t, filepath.Join(wantRepoPath, "hooks", "pre-receive"), "sshdockd git-pre-receive")
	assertReceiveHook(t, filepath.Join(wantRepoPath, "hooks", "post-receive"), "sshdockd git-hook", "my-app", wantRepoPath)
}

func assertReceiveHook(t *testing.T, hookPath string, contents ...string) {
	t.Helper()

	hook, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	for _, content := range contents {
		if !strings.Contains(string(hook), content) {
			t.Fatalf("hook missing %q:\n%s", content, hook)
		}
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

func TestRepoManagerInstallHooksRepairsExistingHookPermissions(t *testing.T) {
	repoPath := t.TempDir()
	hookDir := filepath.Join(repoPath, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("MkdirAll hooks: %v", err)
	}
	for _, name := range []string{"pre-receive", "post-receive"} {
		if err := os.WriteFile(filepath.Join(hookDir, name), []byte("stale\n"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	manager := NewRepoManager(RepoManagerConfig{})
	if err := manager.InstallHooks("my-app", repoPath); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	assertReceiveHook(t, filepath.Join(hookDir, "pre-receive"), "sshdockd git-pre-receive")
	assertReceiveHook(t, filepath.Join(hookDir, "post-receive"), "sshdockd git-hook", "my-app", repoPath)
}

func TestRepoManagerUsesDefaultRemoteHost(t *testing.T) {
	manager := NewRepoManager(RepoManagerConfig{AppsDir: t.TempDir(), Executor: &recordingGitExecutor{}})

	if got := manager.RemoteURL("my-app"); got != "git@server:my-app.git" {
		t.Fatalf("RemoteURL = %q", got)
	}
}

func TestRepoManagerSetupBareRepo_transfersConfiguredOwner_when_runningAsRoot(t *testing.T) {
	// Given a root-run explicit setup for the account that receives Git pushes.
	manager := NewRepoManager(RepoManagerConfig{
		AppsDir:   t.TempDir(),
		Executor:  &recordingGitExecutor{},
		OwnerUser: "sshdock",
	})
	manager.isRoot = func() bool { return true }
	manager.lookupOwner = func(name string) (repoOwner, error) {
		if name != "sshdock" {
			t.Fatalf("owner lookup name = %q, want sshdock", name)
		}
		return repoOwner{uid: 123, gid: 456}, nil
	}
	owned := make([]string, 0)
	manager.chown = func(path string, owner repoOwner) error {
		if owner != (repoOwner{uid: 123, gid: 456}) {
			t.Fatalf("owner = %#v", owner)
		}
		owned = append(owned, path)
		return nil
	}

	// When the bare repository and its hooks are created.
	repo, err := manager.SetupBareRepo(context.Background(), "my-app")

	// Then the Git receiver owns the repository and both hooks.
	if err != nil {
		t.Fatalf("SetupBareRepo: %v", err)
	}
	for _, want := range []string{
		filepath.Dir(repo.Path),
		repo.Path,
		filepath.Join(repo.Path, "hooks", "pre-receive"),
		filepath.Join(repo.Path, "hooks", "post-receive"),
	} {
		if !slices.Contains(owned, want) {
			t.Fatalf("owned paths = %#v, missing %q", owned, want)
		}
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

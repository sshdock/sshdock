package gitrecv

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestReceivePackServiceRejectsInvalidAppNameWithRemoteUpdateCommand(t *testing.T) {
	ctx := context.Background()
	sqlite := newReceiveTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	service := NewReceivePackService(ReceivePackServiceConfig{
		Store:       sqlite,
		AppsDir:     appsDir,
		RepoManager: NewRepoManager(RepoManagerConfig{AppsDir: appsDir, GitHost: "sshdock.example.com", Executor: &recordingGitExecutor{}}),
	})

	err := service.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'My_App.git'"})

	want := "app name \"My_App\" is not a normalized DNS label; use \"my-app\"\nrun: git remote set-url sshdock git@sshdock.example.com:my-app.git"
	if err == nil || err.Error() != want {
		t.Fatalf("Receive error = %q, want %q", err, want)
	}
	if _, getErr := sqlite.GetApp(ctx, "my-app"); !errors.Is(getErr, store.ErrNotFound) {
		t.Fatalf("GetApp error = %v, want ErrNotFound", getErr)
	}
	if entries, readErr := filepath.Glob(filepath.Join(appsDir, "*")); readErr != nil || len(entries) != 0 {
		t.Fatalf("app directories = %#v, error = %v, want none", entries, readErr)
	}
}

func TestReceivePackServiceSuggestsNameWhenGitPathContainsSpaces(t *testing.T) {
	ctx := context.Background()
	sqlite := newReceiveTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	service := NewReceivePackService(ReceivePackServiceConfig{
		Store:       sqlite,
		AppsDir:     appsDir,
		RepoManager: NewRepoManager(RepoManagerConfig{AppsDir: appsDir, GitHost: "sshdock.example.com", Executor: &recordingGitExecutor{}}),
	})

	err := service.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'My App.git'"})

	want := "app name \"My App\" is not a normalized DNS label; use \"my-app\"\nrun: git remote set-url sshdock git@sshdock.example.com:my-app.git"
	if err == nil || err.Error() != want {
		t.Fatalf("Receive error = %q, want %q", err, want)
	}
}

func TestReceivePackServiceKeepsExistingFlatLegacyAppReachable(t *testing.T) {
	ctx := context.Background()
	sqlite := newReceiveTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	repoPath := filepath.Join(appsDir, "My_App", "repo.git")
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	if err := sqlite.CreateApp(ctx, app.App{
		ID:           "My_App",
		Name:         "My_App",
		NodeID:       "node-a",
		RepoPath:     repoPath,
		WorktreePath: filepath.Join(appsDir, "My_App", "worktree"),
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	receivePack := &recordingReceivePackRunner{}
	service := NewReceivePackService(ReceivePackServiceConfig{
		Store:             sqlite,
		AppsDir:           appsDir,
		RepoManager:       NewRepoManager(RepoManagerConfig{AppsDir: appsDir, GitHost: "sshdock.example.com", Executor: &recordingGitExecutor{}}),
		ReceivePackRunner: receivePack,
	})

	err := service.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'My_App.git'"})

	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if receivePack.repoPath != repoPath {
		t.Fatalf("receive-pack repoPath = %q, want %q", receivePack.repoPath, repoPath)
	}
}

func TestReceivePackServiceRejectsRuntimeIdentityCollisionWithLegacyApp(t *testing.T) {
	ctx := context.Background()
	sqlite := newReceiveTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	if err := sqlite.CreateApp(ctx, app.App{
		ID:        "foo.bar",
		Name:      "foo.bar",
		NodeID:    "node-a",
		RepoPath:  filepath.Join(t.TempDir(), "foo.bar", "repo.git"),
		Status:    app.AppStatusCreated,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	appsDir := filepath.Join(t.TempDir(), "apps")
	service := NewReceivePackService(ReceivePackServiceConfig{
		Store:       sqlite,
		AppsDir:     appsDir,
		RepoManager: NewRepoManager(RepoManagerConfig{AppsDir: appsDir, GitHost: "sshdock.example.com", Executor: &recordingGitExecutor{}}),
	})

	err := service.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'foo-bar.git'"})

	want := `app name "foo-bar" conflicts with existing app "foo.bar" because both use runtime identity "sshdock_foo-bar"; choose another app name`
	if err == nil || err.Error() != want {
		t.Fatalf("Receive error = %q, want %q", err, want)
	}
	if _, getErr := sqlite.GetApp(ctx, "foo-bar"); !errors.Is(getErr, store.ErrNotFound) {
		t.Fatalf("GetApp(foo-bar) error = %v, want ErrNotFound", getErr)
	}
}

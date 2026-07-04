package gitrecv

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestParseReceivePackCommand(t *testing.T) {
	appName, err := ParseReceivePackCommand("git-receive-pack 'test-app.git'")
	if err != nil {
		t.Fatalf("ParseReceivePackCommand: %v", err)
	}
	if appName != "test-app" {
		t.Fatalf("appName = %q, want test-app", appName)
	}
}

func TestParseReceivePackCommandRejectsNamespacePath(t *testing.T) {
	_, err := ParseReceivePackCommand("git-receive-pack 'ike/test-app.git'")
	if err == nil {
		t.Fatal("ParseReceivePackCommand succeeded, want error")
	}
}

func TestParseReceivePackCommandRejectsUnsupportedCommand(t *testing.T) {
	_, err := ParseReceivePackCommand("git-upload-pack 'test-app.git'")
	if err == nil {
		t.Fatal("ParseReceivePackCommand succeeded, want error")
	}
}

func TestReceivePackServiceCreatesMissingAppAndRunsReceivePack(t *testing.T) {
	ctx := context.Background()
	sqlite := newReceiveTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	executor := &recordingGitExecutor{}
	receivePack := &recordingReceivePackRunner{}
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	service := NewReceivePackService(ReceivePackServiceConfig{
		Store:             sqlite,
		AppsDir:           appsDir,
		NodeID:            "node-a",
		RepoManager:       NewRepoManager(RepoManagerConfig{AppsDir: appsDir, GitHost: "sshdock.example.com", Executor: executor}),
		ReceivePackRunner: receivePack,
		Now:               func() time.Time { return now },
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := service.Receive(ctx, ReceivePackRequest{
		OriginalCommand: "git-receive-pack 'test-app.git'",
		Stdin:           bytes.NewBufferString("pack input"),
		Stdout:          &stdout,
		Stderr:          &stderr,
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}

	model, err := sqlite.GetApp(ctx, "test-app")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	wantRepoPath := filepath.Join(appsDir, "test-app", "repo.git")
	wantApp := app.App{
		ID:           "test-app",
		Name:         "test-app",
		NodeID:       "node-a",
		RepoPath:     wantRepoPath,
		WorktreePath: filepath.Join(appsDir, "test-app", "worktree"),
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if model != wantApp {
		t.Fatalf("app = %#v, want %#v", model, wantApp)
	}
	if len(executor.Commands) != 1 {
		t.Fatalf("git commands = %#v, want one git init", executor.Commands)
	}
	if receivePack.repoPath != wantRepoPath {
		t.Fatalf("receive-pack repoPath = %q, want %q", receivePack.repoPath, wantRepoPath)
	}
	if receivePack.stdin != "pack input" {
		t.Fatalf("receive-pack stdin = %q", receivePack.stdin)
	}
}

func TestReceivePackServiceReusesExistingAppRepo(t *testing.T) {
	ctx := context.Background()
	sqlite := newReceiveTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	repoPath := filepath.Join(appsDir, "test-app", "repo.git")
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	if err := sqlite.CreateApp(ctx, app.App{
		ID:           "test-app",
		Name:         "test-app",
		NodeID:       "node-a",
		RepoPath:     repoPath,
		WorktreePath: filepath.Join(appsDir, "test-app", "worktree"),
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	executor := &recordingGitExecutor{}
	receivePack := &recordingReceivePackRunner{}
	service := NewReceivePackService(ReceivePackServiceConfig{
		Store:             sqlite,
		AppsDir:           appsDir,
		NodeID:            "node-a",
		RepoManager:       NewRepoManager(RepoManagerConfig{AppsDir: appsDir, GitHost: "sshdock.example.com", Executor: executor}),
		ReceivePackRunner: receivePack,
	})

	err := service.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'test-app.git'"})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if len(executor.Commands) != 0 {
		t.Fatalf("git commands = %#v, want none for existing app", executor.Commands)
	}
	if receivePack.repoPath != repoPath {
		t.Fatalf("receive-pack repoPath = %q, want %q", receivePack.repoPath, repoPath)
	}
}

func TestReceivePackServiceReturnsReceivePackError(t *testing.T) {
	ctx := context.Background()
	sqlite := newReceiveTestStore(t, ctx)
	failure := errors.New("receive-pack failed")
	receivePack := &recordingReceivePackRunner{err: failure}
	appsDir := filepath.Join(t.TempDir(), "apps")
	service := NewReceivePackService(ReceivePackServiceConfig{
		Store:             sqlite,
		AppsDir:           appsDir,
		NodeID:            "node-a",
		RepoManager:       NewRepoManager(RepoManagerConfig{AppsDir: appsDir, Executor: &recordingGitExecutor{}}),
		ReceivePackRunner: receivePack,
	})

	err := service.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'test-app.git'"})
	if !errors.Is(err, failure) {
		t.Fatalf("Receive error = %v, want %v", err, failure)
	}
}

type recordingReceivePackRunner struct {
	repoPath string
	stdin    string
	err      error
}

func (r *recordingReceivePackRunner) RunReceivePack(_ context.Context, repoPath string, stdinReader io.Reader, _ io.Writer, _ io.Writer) error {
	r.repoPath = repoPath
	if stdinReader != nil {
		data, _ := io.ReadAll(stdinReader)
		r.stdin = string(data)
	}
	return r.err
}

func newReceiveTestStore(t *testing.T, ctx context.Context) *store.SQLiteStore {
	t.Helper()

	sqlite, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlite.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	return sqlite
}

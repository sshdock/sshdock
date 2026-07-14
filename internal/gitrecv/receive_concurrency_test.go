package gitrecv

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/deploycoord"
)

func TestReceivePackServiceRejectsSecondPushWhenSameAppIsReceiving(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newReceiveTestStore(t, ctx)
	rootDir := t.TempDir()
	appsDir := filepath.Join(rootDir, "apps")
	locksDir := filepath.Join(rootDir, "locks")
	repoPath := filepath.Join(appsDir, "test-app", "repo.git")
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	if err := sqlite.CreateApp(ctx, app.App{
		ID:           "test-app",
		Name:         "test-app",
		NodeID:       "local",
		RepoPath:     repoPath,
		WorktreePath: filepath.Join(appsDir, "test-app", "worktree"),
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	firstRunner := newBlockingReceivePackRunner()
	firstService := NewReceivePackService(ReceivePackServiceConfig{
		Store:             sqlite,
		AppsDir:           appsDir,
		LocksDir:          locksDir,
		RepoManager:       NewRepoManager(RepoManagerConfig{AppsDir: appsDir}),
		ReceivePackRunner: firstRunner,
	})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- firstService.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'test-app.git'"})
	}()
	<-firstRunner.started

	secondRunner := &recordingReceivePackRunner{}
	secondService := NewReceivePackService(ReceivePackServiceConfig{
		Store:             sqlite,
		AppsDir:           appsDir,
		LocksDir:          locksDir,
		RepoManager:       NewRepoManager(RepoManagerConfig{AppsDir: appsDir}),
		ReceivePackRunner: secondRunner,
	})

	// When
	secondErr := secondService.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'test-app.git'"})

	// Then
	if secondErr == nil || !strings.Contains(secondErr.Error(), `another push is already running for app "test-app"`) {
		t.Fatalf("second Receive error = %v, want actionable same-app rejection", secondErr)
	}
	if secondRunner.repoPath != "" {
		t.Fatalf("second receive-pack repo path = %q, want runner not started", secondRunner.repoPath)
	}
	close(firstRunner.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Receive: %v", err)
	}
	if err := secondService.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'test-app.git'"}); err != nil {
		t.Fatalf("Receive after first push released lock: %v", err)
	}
}

func TestReceivePackServiceWaitsForDeploymentBeforeStartingReceivePack(t *testing.T) {
	// Given
	ctx := context.Background()
	rootDir := t.TempDir()
	appsDir := filepath.Join(rootDir, "apps")
	locksDir := filepath.Join(rootDir, "locks")
	manager := deploycoord.NewManager(locksDir)
	active, err := manager.AcquireDeployment(ctx, nil)
	if err != nil {
		t.Fatalf("AcquireDeployment: %v", err)
	}
	service := NewReceivePackService(ReceivePackServiceConfig{
		Store:             newReceiveTestStore(t, ctx),
		AppsDir:           appsDir,
		LocksDir:          locksDir,
		RepoManager:       NewRepoManager(RepoManagerConfig{AppsDir: appsDir}),
		ReceivePackRunner: &recordingReceivePackRunner{},
	})
	waitOutput := newReceiveWaitSignalWriter("deploy: waiting for another app deployment to finish")
	waitCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- service.Receive(waitCtx, ReceivePackRequest{
			OriginalCommand: "git-receive-pack 'waiting-app.git'",
			Stderr:          waitOutput,
		})
	}()
	select {
	case <-waitOutput.notified:
	case <-time.After(2 * time.Second):
		t.Fatal("receive did not report deployment wait")
	}

	// When
	if _, err := os.Stat(filepath.Join(appsDir, "waiting-app", "repo.git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repo stat error = %v, want receive-pack setup not started", err)
	}
	cancel()

	// Then
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Receive error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Receive did not stop after cancellation")
	}
	if err := active.Release(); err != nil {
		t.Fatalf("release active deployment: %v", err)
	}
	appGuard, err := manager.AcquireApp(ctx, "waiting-app")
	if err != nil {
		t.Fatalf("AcquireApp after canceled receive: %v", err)
	}
	if err := appGuard.Release(); err != nil {
		t.Fatalf("release app guard: %v", err)
	}
}

type blockingReceivePackRunner struct {
	started chan struct{}
	release chan struct{}
}

func newBlockingReceivePackRunner() *blockingReceivePackRunner {
	return &blockingReceivePackRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (r *blockingReceivePackRunner) RunReceivePack(_ context.Context, _ string, _ io.Reader, _ io.Writer, _ io.Writer) error {
	close(r.started)
	<-r.release
	return nil
}

type receiveWaitSignalWriter struct {
	want     string
	notified chan struct{}
	once     sync.Once
}

func newReceiveWaitSignalWriter(want string) *receiveWaitSignalWriter {
	return &receiveWaitSignalWriter{want: want, notified: make(chan struct{})}
}

func (w *receiveWaitSignalWriter) Write(data []byte) (int, error) {
	if strings.Contains(string(data), w.want) {
		w.once.Do(func() { close(w.notified) })
	}
	return len(data), nil
}

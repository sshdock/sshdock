package gitrecv

import (
	"context"
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

func TestReceivePackServiceStartsReceivePackWhileDeploymentIsActive(t *testing.T) {
	// Given
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
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
	// When
	err = service.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'waiting-app.git'"})

	// Then
	if err != nil {
		t.Fatalf("Receive while another deployment is active: %v", err)
	}
	if err := active.Release(); err != nil {
		t.Fatalf("release active deployment: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appsDir, "waiting-app", "repo.git")); err != nil {
		t.Fatalf("repo stat: %v", err)
	}
}

func TestReceivePackServiceReleasesAppLockBeforeLogAttachment(t *testing.T) {
	// Given
	ctx := context.Background()
	rootDir := t.TempDir()
	appsDir := filepath.Join(rootDir, "apps")
	locksDir := filepath.Join(rootDir, "locks")
	repoPath := filepath.Join(appsDir, "test-app", "repo.git")
	now := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	sqlite := newReceiveTestStore(t, ctx)
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
	firstRunner := &recordingReceivePackRunner{run: func() error {
		deployment := app.Deployment{ID: "dep_attached", AppID: "test-app", ReleaseID: app.ReleaseID("test-app", "abc123"), CommitSHA: "abc123", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now}
		if err := sqlite.QueueDeployment(ctx, deployment, ""); err != nil {
			return err
		}
		if err := sqlite.RecordDeploymentQueued(ctx,
			app.Event{ID: EventID(deployment.ID, "git_ref_accepted"), AppID: deployment.AppID, Type: "git.ref_accepted", Message: "Remote main accepted", CreatedAt: now},
			app.Event{ID: EventID(deployment.ID, "queued"), AppID: deployment.AppID, Type: "deploy.queued", Message: "Deploy queued", CreatedAt: now},
		); err != nil {
			return err
		}
		if err := sqlite.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "deploy: succeeded\n", now); err != nil {
			return err
		}
		return sqlite.UpdateDeploymentStatus(ctx, deployment.ID, app.DeploymentStatusSucceeded, now, "")
	}}
	firstService := NewReceivePackService(ReceivePackServiceConfig{Store: sqlite, AppsDir: appsDir, LocksDir: locksDir, RepoManager: NewRepoManager(RepoManagerConfig{AppsDir: appsDir}), ReceivePackRunner: firstRunner})
	attachment := &blockingStreamWriter{started: make(chan struct{}), release: make(chan struct{})}
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- firstService.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'test-app.git'", Stderr: attachment})
	}()
	<-attachment.started
	secondRunner := &recordingReceivePackRunner{}
	secondService := NewReceivePackService(ReceivePackServiceConfig{Store: sqlite, AppsDir: appsDir, LocksDir: locksDir, RepoManager: NewRepoManager(RepoManagerConfig{AppsDir: appsDir}), ReceivePackRunner: secondRunner})

	// When
	secondErr := secondService.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'test-app.git'"})

	// Then
	if secondErr != nil {
		t.Fatalf("second Receive while first client is attached: %v", secondErr)
	}
	if secondRunner.repoPath != repoPath {
		t.Fatalf("second receive-pack repo path = %q, want %q", secondRunner.repoPath, repoPath)
	}
	close(attachment.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Receive: %v", err)
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

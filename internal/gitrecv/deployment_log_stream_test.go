package gitrecv

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestDeploymentLogStreamerFollowsUntilTerminalStatus(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite, deployment := seedDeploymentLogStream(t, ctx)
	polls := make(chan struct{}, 1)
	streamer := NewDeploymentLogStreamer(DeploymentLogStreamerConfig{
		Store: sqlite,
		Poll: func(context.Context) error {
			<-polls
			return nil
		},
	})
	output := &streamSignalWriter{written: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() {
		done <- streamer.Stream(ctx, DeploymentLogStreamRequest{
			AppID:        deployment.AppID,
			DeploymentID: deployment.ID,
			Follow:       true,
		}, output)
	}()
	<-output.written
	if err := sqlite.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "deploy: succeeded\n", deployment.StartedAt.Add(time.Second)); err != nil {
		t.Fatalf("AppendDeploymentLog terminal: %v", err)
	}
	if err := sqlite.UpdateDeploymentStatus(ctx, deployment.ID, app.DeploymentStatusSucceeded, deployment.StartedAt.Add(time.Second), ""); err != nil {
		t.Fatalf("UpdateDeploymentStatus: %v", err)
	}

	// When
	polls <- struct{}{}

	// Then
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deployment log stream did not finish")
	}
	if got := output.String(); got != "deploy: daemon started\ndeploy: succeeded\n" {
		t.Fatalf("stream output = %q", got)
	}
}

func TestDeploymentLogStreamerReloadsOutputAfterTerminalStatus(t *testing.T) {
	// Given
	store := &terminalRaceDeploymentLogStore{}
	streamer := NewDeploymentLogStreamer(DeploymentLogStreamerConfig{Store: store})
	var output bytes.Buffer

	// When
	err := streamer.Stream(context.Background(), DeploymentLogStreamRequest{AppID: "my-app", DeploymentID: "dep_terminal", Follow: true}, &output)

	// Then
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got := output.String(); got != "deploy: daemon started\ndeploy: succeeded\n" {
		t.Fatalf("stream output = %q", got)
	}
}

func TestDeploymentLogStreamerSlowFollowerDoesNotBlockAnotherFollower(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite, deployment := seedDeploymentLogStream(t, ctx)
	slowPolls := make(chan struct{}, 1)
	fastPolls := make(chan struct{}, 1)
	slowStreamer := NewDeploymentLogStreamer(DeploymentLogStreamerConfig{Store: sqlite, Poll: channelPoll(slowPolls)})
	fastStreamer := NewDeploymentLogStreamer(DeploymentLogStreamerConfig{Store: sqlite, Poll: channelPoll(fastPolls)})
	slowOutput := &blockingStreamWriter{started: make(chan struct{}), release: make(chan struct{})}
	fastOutput := &streamSignalWriter{written: make(chan struct{}, 1)}
	slowDone := make(chan error, 1)
	fastDone := make(chan error, 1)
	go func() {
		slowDone <- slowStreamer.Stream(ctx, DeploymentLogStreamRequest{AppID: deployment.AppID, DeploymentID: deployment.ID, Follow: true}, slowOutput)
	}()
	<-slowOutput.started
	go func() {
		fastDone <- fastStreamer.Stream(ctx, DeploymentLogStreamRequest{AppID: deployment.AppID, DeploymentID: deployment.ID, Follow: true}, fastOutput)
	}()
	<-fastOutput.written
	if err := sqlite.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "deploy: succeeded\n", deployment.StartedAt.Add(time.Second)); err != nil {
		t.Fatalf("AppendDeploymentLog terminal: %v", err)
	}
	if err := sqlite.UpdateDeploymentStatus(ctx, deployment.ID, app.DeploymentStatusSucceeded, deployment.StartedAt.Add(time.Second), ""); err != nil {
		t.Fatalf("UpdateDeploymentStatus: %v", err)
	}

	// When
	fastPolls <- struct{}{}

	// Then
	select {
	case err := <-fastDone:
		if err != nil {
			t.Fatalf("fast Stream: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fast follower was blocked by the slow follower")
	}
	close(slowOutput.release)
	slowPolls <- struct{}{}
	select {
	case err := <-slowDone:
		if err != nil {
			t.Fatalf("slow Stream: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow follower did not finish after release")
	}
}

func TestDeploymentAttachmentErrorNamesAppBeforeDeploymentSelection(t *testing.T) {
	// Given
	err := &DeploymentAttachmentError{AppID: "my-app", Err: context.Canceled}

	// When
	message := err.Error()

	// Then
	if message != `deployment log attachment for app "my-app" stopped before deployment selection: context canceled` {
		t.Fatalf("attachment error = %q", message)
	}
}

func seedDeploymentLogStream(t *testing.T, ctx context.Context) (*store.SQLiteStore, app.Deployment) {
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
	now := time.Date(2026, 7, 26, 13, 0, 0, 0, time.UTC)
	if err := sqlite.CreateApp(ctx, app.App{ID: "my-app", Name: "my-app", NodeID: "node-a", Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	deployment := app.Deployment{
		ID:        "dep_stream",
		AppID:     "my-app",
		ReleaseID: app.ReleaseID("my-app", "abc123"),
		CommitSHA: "abc123",
		Trigger:   app.DeploymentTriggerPush,
		Status:    app.DeploymentStatusPending,
		StartedAt: now,
	}
	if err := sqlite.QueueDeployment(ctx, deployment, ""); err != nil {
		t.Fatalf("QueueDeployment: %v", err)
	}
	if err := sqlite.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "deploy: daemon started\n", now); err != nil {
		t.Fatalf("AppendDeploymentLog initial: %v", err)
	}
	return sqlite, deployment
}

func channelPoll(polls <-chan struct{}) func(context.Context) error {
	return func(context.Context) error {
		<-polls
		return nil
	}
}

type streamSignalWriter struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	written chan struct{}
}

func (w *streamSignalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buffer.Write(p)
	w.mu.Unlock()
	select {
	case w.written <- struct{}{}:
	default:
	}
	return n, err
}

func (w *streamSignalWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

type blockingStreamWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type terminalRaceDeploymentLogStore struct {
	loads int
}

func (s *terminalRaceDeploymentLogStore) DeploymentLog(context.Context, string, string) (app.DeploymentLog, error) {
	s.loads++
	content := "deploy: daemon started\n"
	if s.loads > 1 {
		content += "deploy: succeeded\n"
	}
	return app.DeploymentLog{DeploymentID: "dep_terminal", AppID: "my-app", Content: content}, nil
}

func (*terminalRaceDeploymentLogStore) ListDeploymentsByApp(context.Context, string) ([]app.Deployment, error) {
	return []app.Deployment{{ID: "dep_terminal", AppID: "my-app", Status: app.DeploymentStatusSucceeded}}, nil
}

func (w *blockingStreamWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(p), nil
}

var _ io.Writer = (*streamSignalWriter)(nil)
var _ io.Writer = (*blockingStreamWriter)(nil)

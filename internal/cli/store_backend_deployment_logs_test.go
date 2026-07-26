package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestStoreBackendDeploymentLogsReadsLatestAndRejectsUnknownAttempt(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, t.TempDir(), now)
	for _, deployment := range []app.Deployment{
		{ID: "dep_old", AppID: "my-app", ReleaseID: "rel_old", CommitSHA: "old", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now},
		{ID: "dep_new", AppID: "my-app", ReleaseID: "rel_new", CommitSHA: "new", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now.Add(time.Minute)},
	} {
		if err := sqlite.QueueDeployment(ctx, deployment, "previous_"+deployment.ID); err != nil {
			t.Fatalf("QueueDeployment %q: %v", deployment.ID, err)
		}
		if err := sqlite.UpdateDeploymentStatus(ctx, deployment.ID, app.DeploymentStatusSucceeded, deployment.StartedAt, ""); err != nil {
			t.Fatalf("UpdateDeploymentStatus %q: %v", deployment.ID, err)
		}
		if err := sqlite.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, deployment.ID+" output\n", deployment.StartedAt); err != nil {
			t.Fatalf("AppendDeploymentLog %q: %v", deployment.ID, err)
		}
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{})

	// When
	var stdout bytes.Buffer
	err := backend.DeploymentLogs(DeploymentLogRequest{AppName: "my-app", Follow: true}, &stdout)

	// Then
	if err != nil {
		t.Fatalf("DeploymentLogs latest: %v", err)
	}
	if stdout.String() != "dep_new output\n" {
		t.Fatalf("latest deployment output = %q", stdout.String())
	}
	var missing bytes.Buffer
	err = backend.DeploymentLogs(DeploymentLogRequest{AppName: "my-app", DeploymentID: "dep_missing"}, &missing)
	if err == nil || !strings.Contains(err.Error(), `deployment "dep_missing" not found for app "my-app"`) {
		t.Fatalf("unknown deployment error = %v", err)
	}
}

func TestStoreBackendDeploymentLogsFollowReadsTerminalOutput(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, t.TempDir(), now)
	deployment := app.Deployment{ID: "dep_follow", AppID: "my-app", ReleaseID: "rel_follow", CommitSHA: "follow", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now}
	if err := sqlite.QueueDeployment(ctx, deployment, "previous"); err != nil {
		t.Fatalf("QueueDeployment: %v", err)
	}
	if err := sqlite.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "deploy: started\n", now); err != nil {
		t.Fatalf("AppendDeploymentLog start: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{})
	output := &notifyingBuffer{firstWrite: make(chan struct{}, 1)}
	done := make(chan error, 1)

	// When
	go func() {
		done <- backend.DeploymentLogs(DeploymentLogRequest{AppName: "my-app", DeploymentID: deployment.ID, Follow: true}, output)
	}()
	<-output.firstWrite
	if err := sqlite.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "deploy: succeeded\n", now.Add(time.Second)); err != nil {
		t.Fatalf("AppendDeploymentLog terminal: %v", err)
	}
	if err := sqlite.UpdateDeploymentStatus(ctx, deployment.ID, app.DeploymentStatusSucceeded, now.Add(time.Second), ""); err != nil {
		t.Fatalf("UpdateDeploymentStatus: %v", err)
	}

	// Then
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeploymentLogs follow: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DeploymentLogs follow did not finish after the terminal status")
	}
	if output.String() != "deploy: started\ndeploy: succeeded\n" {
		t.Fatalf("follow output = %q", output.String())
	}
}

type notifyingBuffer struct {
	bytes.Buffer
	firstWrite chan struct{}
}

func (b *notifyingBuffer) Write(p []byte) (int, error) {
	n, err := b.Buffer.Write(p)
	select {
	case b.firstWrite <- struct{}{}:
	default:
	}
	return n, err
}

func (b *notifyingBuffer) WriteString(value string) (int, error) {
	return b.Write([]byte(value))
}

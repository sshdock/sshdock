package gitrecv

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestReceivePackServiceRecoversAndFollowsPendingDeployment(t *testing.T) {
	ctx := context.Background()
	sqlite := newReceiveTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	var deployment app.Deployment
	service := NewReceivePackService(ReceivePackServiceConfig{
		Store: sqlite, AppsDir: appsDir,
		RepoManager: NewRepoManager(RepoManagerConfig{AppsDir: appsDir, Executor: &recordingGitExecutor{}}),
		ReceivePackRunner: &recordingReceivePackRunner{run: func() error {
			deployment = reservePendingRecovery(t, sqlite, "test-app", "dep_original", "abc123")
			return sqlite.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "original log\n", deployment.StartedAt)
		}},
		CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) { return "abc123", nil }),
	})
	service.deploymentLogs.poll = func(ctx context.Context) error {
		events, err := sqlite.ListEventsByApp(ctx, deployment.AppID)
		if err != nil || len(events) != 2 {
			t.Fatalf("events at attachment = %#v, %v; want accepted and queued", events, err)
		}
		if err := sqlite.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "deploy: succeeded\n", time.Now()); err != nil {
			return err
		}
		return sqlite.UpdateDeploymentStatus(ctx, deployment.ID, app.DeploymentStatusSucceeded, time.Now(), "")
	}
	var output bytes.Buffer
	if err := service.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'test-app.git'", Stderr: &output}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	for _, want := range []string{"deploy: following dep_original", "original log", "deploy: succeeded"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
	deployments, err := sqlite.ListDeploymentsByApp(ctx, "test-app")
	if err != nil || len(deployments) != 1 || deployments[0].ID != deployment.ID {
		t.Fatalf("deployments = %#v, %v; want original attempt", deployments, err)
	}
}

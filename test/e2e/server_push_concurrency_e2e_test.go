//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestServerPushRejectsSameAppWhileDeploymentIsActive(t *testing.T) {
	// Given
	paths := setupBootstrappedServerPush(t, "fake")
	appName := "busy-app"
	initialCommit := pushComposeAppThroughSSH(t, paths, appName, map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:initial\n",
	})
	db, err := store.OpenSQLite(context.Background(), filepath.Join(paths.dataDir, "sshdock.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := db.CreateDeployment(context.Background(), app.Deployment{
		ID:        "dep_busy",
		AppID:     appName,
		ReleaseID: app.ReleaseID(appName, "busy"),
		CommitSHA: "busy",
		Trigger:   app.DeploymentTriggerPush,
		Status:    app.DeploymentStatusDeploying,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		_ = db.Close()
		t.Fatalf("CreateDeployment: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	sourceDir := filepath.Join(paths.tmp, "source-"+appName)
	writeCurrentMainCompose(t, sourceDir, "example/web:second")
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "Second deploy")

	// When
	output, pushErr := runCurrentMainGitAttempt(sourceDir, currentMainPushEnv(t, paths), "push", "sshdock", "main")

	// Then
	if pushErr == nil || !strings.Contains(output, "already has a pending or active deployment") {
		t.Fatalf("same-app push error = %v, output:\n%s", pushErr, output)
	}
	assertRemoteMain(t, filepath.Join(paths.dataDir, "apps", appName, "repo.git"), initialCommit)

	otherApp := "other-app"
	otherCommit := pushComposeAppThroughSSH(t, paths, otherApp, map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	})
	assertRemoteMain(t, filepath.Join(paths.dataDir, "apps", otherApp, "repo.git"), otherCommit)
}

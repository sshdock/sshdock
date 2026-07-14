package cli

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestStoreBackendRedeployCreatesDistinctAttemptsForSameRelease(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	ids := []string{"dep_first", "dep_second"}
	nextID := 0
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:         "node-a",
		AppsDir:        appsDir,
		RecoveryRunner: &compose.FakeRunner{},
		Now:            func() time.Time { return now },
		NewDeploymentID: func() (string, error) {
			id := ids[nextID]
			nextID++
			return id, nil
		},
	})

	// When
	if err := backend.RedeployApp("my-app"); err != nil {
		t.Fatalf("RedeployApp first: %v", err)
	}
	if err := backend.RedeployApp("my-app"); err != nil {
		t.Fatalf("RedeployApp second: %v", err)
	}

	// Then
	deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 2 {
		t.Fatalf("deployments = %#v", deployments)
	}
	for index, deployment := range deployments {
		if deployment.ID != ids[index] || deployment.ReleaseID != "rel_new" || deployment.CommitSHA != "new" || deployment.Trigger != app.DeploymentTriggerRedeploy || deployment.Status != app.DeploymentStatusSucceeded {
			t.Fatalf("deployment[%d] = %#v", index, deployment)
		}
	}
}

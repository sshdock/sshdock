package cli

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
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
		NodeID:              "node-a",
		AppsDir:             appsDir,
		RecoveryRunner:      &compose.FakeRunner{},
		CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) { return "new", nil }),
		Now:                 func() time.Time { return now },
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

func TestStoreBackendRedeployAttachesInitialRouteAfterConfigRecovery(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{BaseDomain: "example.com", GitHost: "sshdock.example.com"}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 26, 7, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	router := &fakeRoutePublisher{}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID:              "node-a",
		AppsDir:             appsDir,
		RecoveryRunner:      &compose.FakeRunner{DeployResult: compose.DeployResult{RouteFound: true, RouteTarget: compose.RouteTarget{ServiceName: "web", Port: 3000}}},
		CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) { return "new", nil }),
		Router:              router,
		Now:                 func() time.Time { return now },
		NewDeploymentID: func() (string, error) {
			return "dep_redeploy", nil
		},
	})

	if err := backend.RedeployApp("my-app"); err != nil {
		t.Fatalf("RedeployApp: %v", err)
	}

	domains, err := sqlite.ListDomainsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 1 || domains[0].DomainName != "my-app.example.com" || domains[0].ServiceName != "web" || domains[0].Port != 3000 {
		t.Fatalf("domains = %#v", domains)
	}
	if len(router.Syncs) != 1 {
		t.Fatalf("route syncs = %#v", router.Syncs)
	}
}

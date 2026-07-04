package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestServiceReleaseHistoryIdentifiesCurrentAndLastSuccessful(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	service := NewService(store)
	old := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
	newer := old.Add(time.Hour)
	store.releases["rel_1"] = Release{ID: "rel_1", AppID: "app_1", Status: ReleaseStatusSucceeded, CreatedAt: old}
	store.releases["rel_2"] = Release{ID: "rel_2", AppID: "app_1", Status: ReleaseStatusFailed, CreatedAt: newer}

	history, err := service.ReleaseHistory(ctx, "app_1")
	if err != nil {
		t.Fatalf("ReleaseHistory: %v", err)
	}

	if len(history.Releases) != 2 {
		t.Fatalf("Releases = %#v", history.Releases)
	}
	if history.CurrentRelease == nil || history.CurrentRelease.ID != "rel_1" {
		t.Fatalf("CurrentRelease = %#v", history.CurrentRelease)
	}
	if history.LastSuccessfulRelease == nil || history.LastSuccessfulRelease.ID != "rel_1" {
		t.Fatalf("LastSuccessfulRelease = %#v", history.LastSuccessfulRelease)
	}
}

func TestServiceRollbackReleaseDeploysAndMarksSucceeded(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	runner := &compose.FakeRunner{}
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusFailed}
	store.releases["rel_1"] = Release{
		ID:          "rel_1",
		AppID:       "app_1",
		CommitSHA:   "abc123",
		ComposePath: "compose.yml",
		Status:      ReleaseStatusSucceeded,
	}

	deployment, err := service.RollbackRelease(ctx, "app_1", "rel_1", "dep_rollback_1")
	if err != nil {
		t.Fatalf("RollbackRelease: %v", err)
	}

	if deployment.Status != DeploymentStatusSucceeded {
		t.Fatalf("deployment status = %q", deployment.Status)
	}
	if len(runner.DeployRequests) != 1 {
		t.Fatalf("DeployRequests = %#v", runner.DeployRequests)
	}
	request := runner.DeployRequests[0]
	if request.AppName != "app_1" || request.ReleaseID != "rel_1" || request.CommitSHA != "abc123" || request.ComposePath != "compose.yml" || request.ProjectDir != "/apps/app_1/worktree" {
		t.Fatalf("DeployRequest = %#v", request)
	}
	if store.deploymentStatuses["dep_rollback_1"] != DeploymentStatusSucceeded {
		t.Fatalf("stored deployment status = %q", store.deploymentStatuses["dep_rollback_1"])
	}
	assertEventTypes(t, store.events["app_1"], []string{"rollback.triggered", "rollback.succeeded"})
}

func TestServiceRollbackReleaseMarksFailedWhenDeployFails(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	failure := errors.New("deploy failed")
	runner := &compose.FakeRunner{DeployErr: failure}
	service := NewService(store, WithDeployRunner(runner))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: "/apps/app_1/worktree", Status: AppStatusHealthy}
	store.releases["rel_1"] = Release{
		ID:          "rel_1",
		AppID:       "app_1",
		CommitSHA:   "abc123",
		ComposePath: "compose.yml",
		Status:      ReleaseStatusSucceeded,
	}

	_, err := service.RollbackRelease(ctx, "app_1", "rel_1", "dep_rollback_1")
	if !errors.Is(err, failure) {
		t.Fatalf("RollbackRelease error = %v, want %v", err, failure)
	}
	if store.deploymentStatuses["dep_rollback_1"] != DeploymentStatusFailed {
		t.Fatalf("stored deployment status = %q", store.deploymentStatuses["dep_rollback_1"])
	}
	if store.deploymentErrors["dep_rollback_1"] != "deploy failed" {
		t.Fatalf("stored deployment error = %q", store.deploymentErrors["dep_rollback_1"])
	}
	assertEventTypes(t, store.events["app_1"], []string{"rollback.triggered", "rollback.failed"})
}

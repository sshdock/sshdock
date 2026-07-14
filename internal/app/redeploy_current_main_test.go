package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceRedeployCurrentMainCreatesReleaseForUnrecordedCommit(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	worktreePath := t.TempDir()
	runner := &fakeRecoveryRunner{}
	checkout := checkoutCallback(func() error {
		return os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte("services: {}\n"), 0o644)
	})
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithWorktreeCheckout(checkout), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", RepoPath: "/apps/app_1/repo.git", WorktreePath: worktreePath, Status: AppStatusFailed}

	// When
	deployment, err := service.RedeployCurrentMain(ctx, "app_1", "dep_redeploy_1")

	// Then
	if err != nil {
		t.Fatalf("RedeployCurrentMain: %v", err)
	}
	releaseID := ReleaseID("app_1", "new")
	if deployment.ReleaseID != releaseID || deployment.CommitSHA != "new" || deployment.Status != DeploymentStatusSucceeded {
		t.Fatalf("deployment = %#v", deployment)
	}
	if release := store.releases[releaseID]; release.ComposePath != filepath.Join(worktreePath, "compose.yml") || release.Status != ReleaseStatusSucceeded {
		t.Fatalf("release = %#v", release)
	}
	if len(runner.deploys) != 1 || runner.deploys[0].CommitSHA != "new" {
		t.Fatalf("deploy requests = %#v", runner.deploys)
	}
}

func TestServiceRedeployCurrentMainRecordsAttemptWhenComposeMissing(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	runner := &fakeRecoveryRunner{}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithWorktreeCheckout(checkoutCallback(func() error { return nil })), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", RepoPath: "/apps/app_1/repo.git", WorktreePath: t.TempDir(), Status: AppStatusFailed}

	// When
	deployment, err := service.RedeployCurrentMain(ctx, "app_1", "dep_redeploy_1")

	// Then
	if err == nil {
		t.Fatal("RedeployCurrentMain error = nil, want missing Compose failure")
	}
	if deployment.Status != DeploymentStatusFailed || deployment.FailureStage != "detect compose" {
		t.Fatalf("deployment = %#v", deployment)
	}
	if stored := store.deployments[deployment.ID]; stored.Status != DeploymentStatusFailed || stored.FailureStage != "detect compose" {
		t.Fatalf("stored deployment = %#v", stored)
	}
	if len(runner.deploys) != 0 || len(store.releases) != 0 {
		t.Fatalf("deploys = %#v, releases = %#v", runner.deploys, store.releases)
	}
}

func TestServiceRedeployCurrentMainReturnsFailurePersistenceErrors(t *testing.T) {
	ctx := context.Background()
	baseStore := newFakeServiceStore()
	deploymentPersistenceFailure := errors.New("deployment failure was not stored")
	releasePersistenceFailure := errors.New("release failure was not stored")
	store := &failingRedeployStore{
		fakeServiceStore:           baseStore,
		updateDeploymentFailureErr: deploymentPersistenceFailure,
		updateReleaseStatusErr:     releasePersistenceFailure,
	}
	worktreePath := t.TempDir()
	deployFailure := errors.New("compose failed")
	service := NewService(store,
		WithDeployRunner(&fakeRecoveryRunner{deployErr: deployFailure}),
		WithWorktreeCheckout(checkoutCallback(func() error {
			return os.WriteFile(filepath.Join(worktreePath, "compose.yml"), []byte("services: {}\n"), 0o644)
		})),
		withCurrentMain("new"),
	)
	baseStore.apps["app_1"] = App{ID: "app_1", Name: "app_1", RepoPath: "/apps/app_1/repo.git", WorktreePath: worktreePath}

	_, err := service.RedeployCurrentMain(ctx, "app_1", "dep_redeploy_1")

	for _, want := range []error{deployFailure, deploymentPersistenceFailure, releasePersistenceFailure} {
		if !errors.Is(err, want) {
			t.Fatalf("RedeployCurrentMain error = %v, want wrapped %v", err, want)
		}
	}
}

type checkoutCallback func() error

func (f checkoutCallback) Checkout(context.Context, string, string, string) error {
	return f()
}

type failingRedeployStore struct {
	*fakeServiceStore
	updateDeploymentFailureErr error
	updateReleaseStatusErr     error
}

func (f *failingRedeployStore) UpdateDeploymentFailure(context.Context, Deployment) error {
	return f.updateDeploymentFailureErr
}

func (f *failingRedeployStore) UpdateReleaseStatus(context.Context, string, ReleaseStatus, time.Time) error {
	return f.updateReleaseStatusErr
}

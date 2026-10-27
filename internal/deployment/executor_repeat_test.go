package deployment

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	storepkg "github.com/sshdock/sshdock/internal/store"
)

func TestExecutorFailedRetryPreservesSuccessfulRelease(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newExecutorTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	runner := &compose.FakeRunner{}
	ids := []string{"dep_success", "dep_retry"}
	handler := newTestExecutor(t, Config{
		Store:  sqlite,
		Runner: runner,
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})
	if err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", ids[0]); err != nil {
		t.Fatalf("Execute first: %v", err)
	}
	runner.DeployErr = errors.New("retry failed")

	// When
	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", ids[1])

	// Then
	if err == nil {
		t.Fatal("Execute retry error = nil")
	}
	releases, listErr := sqlite.ListReleasesByApp(ctx, "my-app")
	if listErr != nil {
		t.Fatalf("ListReleasesByApp: %v", listErr)
	}
	if len(releases) != 1 || releases[0].Status != app.ReleaseStatusSucceeded {
		t.Fatalf("releases = %#v, want successful release preserved", releases)
	}
	deployments, listErr := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if listErr != nil {
		t.Fatalf("ListDeploymentsByApp: %v", listErr)
	}
	if len(deployments) != 2 || deployments[0].Status != app.DeploymentStatusSucceeded || deployments[1].Status != app.DeploymentStatusFailed {
		t.Fatalf("deployments = %#v", deployments)
	}
}

func TestExecutorRecoversWhenConcurrentPushCreatesReleaseFirst(t *testing.T) {
	ctx := context.Background()
	sqlite := newExecutorTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	racingStore := &concurrentReleaseWinnerStore{SQLiteStore: sqlite}
	handler := newTestExecutor(t, Config{
		Store:  racingStore,
		Runner: &compose.FakeRunner{},
		Checkout: executorCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeExecutorComposeFixture(t, gotWorktreePath)
			return nil
		}),
	})

	err := executeAttempt(t, ctx, sqlite, handler, "my-app", "abc123", "dep_racing_push")

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	releases, err := sqlite.ListReleasesByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListReleasesByApp: %v", err)
	}
	if len(releases) != 1 || releases[0].Status != app.ReleaseStatusSucceeded {
		t.Fatalf("releases = %#v", releases)
	}
	deployments, err := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 1 || deployments[0].Status != app.DeploymentStatusSucceeded {
		t.Fatalf("deployments = %#v", deployments)
	}
}

type concurrentReleaseWinnerStore struct {
	*storepkg.SQLiteStore
	lookupCount int
}

func (s *concurrentReleaseWinnerStore) GetReleaseByAppCommit(ctx context.Context, appID string, commitSHA string) (app.Release, error) {
	s.lookupCount++
	if s.lookupCount == 1 {
		return app.Release{}, storepkg.ErrNotFound
	}
	return s.SQLiteStore.GetReleaseByAppCommit(ctx, appID, commitSHA)
}

func (s *concurrentReleaseWinnerStore) CreateRelease(ctx context.Context, model app.Release) error {
	if err := s.SQLiteStore.CreateRelease(ctx, model); err != nil {
		return err
	}
	return errors.New("concurrent release insert won")
}

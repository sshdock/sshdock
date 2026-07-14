package gitrecv

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	storepkg "github.com/sshdock/sshdock/internal/store"
)

func TestPostReceiveHandlerFailedRetryPreservesSuccessfulRelease(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	runner := &compose.FakeRunner{}
	ids := []string{"dep_success", "dep_retry"}
	nextID := 0
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  sqlite,
		Runner: runner,
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeHookComposeFixture(t, gotWorktreePath)
			return nil
		}),
		NewDeploymentID: func() (string, error) {
			id := ids[nextID]
			nextID++
			return id, nil
		},
	})
	input := "old abc123 refs/heads/main\n"
	if err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", worktreePath, strings.NewReader(input)); err != nil {
		t.Fatalf("Handle first: %v", err)
	}
	runner.DeployErr = errors.New("retry failed")

	// When
	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", worktreePath, strings.NewReader(input))

	// Then
	if err == nil {
		t.Fatal("Handle retry error = nil")
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

func TestPostReceiveHandlerRecoversWhenConcurrentPushCreatesReleaseFirst(t *testing.T) {
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	racingStore := &concurrentReleaseWinnerStore{SQLiteStore: sqlite}
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:  racingStore,
		Runner: &compose.FakeRunner{},
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, gotWorktreePath string, _ string) error {
			writeHookComposeFixture(t, gotWorktreePath)
			return nil
		}),
		NewDeploymentID: func() (string, error) { return "dep_racing_push", nil },
	})

	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", worktreePath, strings.NewReader("old abc123 refs/heads/main\n"))

	if err != nil {
		t.Fatalf("Handle: %v", err)
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

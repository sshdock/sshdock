package gitrecv

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/deploycoord"
	"github.com/sshdock/sshdock/internal/store"
)

func TestQueuedDeploymentWorkerRecoversPendingRef(t *testing.T) {
	for _, tt := range []struct {
		name, main, events string
		resolveErr         error
		busy               bool
		wantWorked         bool
	}{
		{name: "post-receive never ran", main: "abc123", wantWorked: true},
		{name: "accepted event only", main: "abc123", events: "accepted", wantWorked: true},
		{name: "queued bypasses Git", events: "queued", wantWorked: true},
		{name: "ref was never updated", main: "previous"},
		{name: "first push was rejected", resolveErr: ErrMainNotFound},
		{name: "receive still holds lock", main: "abc123", busy: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
			deployment := reservePendingRecovery(t, sqlite, "my-app", "dep_original", "abc123")
			if err := sqlite.AppendDeploymentLog(ctx, deployment.AppID, deployment.ID, "original log\n", deployment.StartedAt); err != nil {
				t.Fatal(err)
			}
			accepted := app.Event{ID: EventID(deployment.ID, "git_ref_accepted"), AppID: deployment.AppID, Type: "git.ref_accepted", CreatedAt: deployment.StartedAt}
			if tt.events == "accepted" {
				if err := sqlite.CreateEvent(ctx, accepted); err != nil {
					t.Fatal(err)
				}
			}
			if tt.events == "queued" {
				if err := sqlite.RecordDeploymentQueued(ctx, accepted, app.Event{ID: EventID(deployment.ID, "queued"), AppID: deployment.AppID, Type: "deploy.queued", CreatedAt: deployment.StartedAt}); err != nil {
					t.Fatal(err)
				}
			}
			locksDir := t.TempDir()
			var guard *deploycoord.Guard
			if tt.busy {
				var err error
				guard, err = deploycoord.NewManager(locksDir).AcquireApp(ctx, deployment.AppID)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if guard != nil {
						_ = guard.Release()
					}
				}()
			}
			resolverCalls := 0
			executor := &recordingQueuedDeploymentExecutor{}
			worker := NewQueuedDeploymentWorker(QueuedDeploymentWorkerConfig{Store: sqlite, Executor: executor, LocksDir: locksDir,
				CurrentMainResolver: app.CurrentMainResolverFunc(func(_ context.Context, repo string) (string, error) {
					resolverCalls++
					if repo != "/apps/my-app/repo.git" {
						t.Fatalf("resolver repo = %q", repo)
					}
					return tt.main, tt.resolveErr
				}),
			})
			worked, err := worker.RunOne(ctx)
			if err != nil || worked != tt.wantWorked {
				t.Fatalf("RunOne = %v, %v; want worked %v", worked, err, tt.wantWorked)
			}
			if (tt.busy || tt.events == "queued") && resolverCalls != 0 {
				t.Fatalf("resolver calls = %d, want none", resolverCalls)
			}
			if tt.busy {
				if err := guard.Release(); err != nil {
					t.Fatal(err)
				}
				guard = nil
				worked, err = worker.RunOne(ctx)
				if err != nil || !worked {
					t.Fatalf("RunOne after release = %v, %v", worked, err)
				}
			}
			if worked {
				if executor.deployment.ID != deployment.ID || executor.deployment.Status != app.DeploymentStatusDeploying {
					t.Fatalf("executor deployment = %#v", executor.deployment)
				}
				events, err := sqlite.ListEventsByApp(ctx, deployment.AppID)
				if err != nil || len(events) != 2 {
					t.Fatalf("events = %#v, %v; want accepted and queued once", events, err)
				}
				log, err := sqlite.DeploymentLog(ctx, deployment.AppID, deployment.ID)
				if err != nil || log.Content != "original log\n" {
					t.Fatalf("log = %#v, %v", log, err)
				}
			} else {
				if executor.deployment.ID != "" {
					t.Fatalf("unexpected execution: %#v", executor.deployment)
				}
				next := deployment
				next.ID, next.CommitSHA, next.ReleaseID = "dep_next", "newsha", app.ReleaseID(deployment.AppID, "newsha")
				if err := sqlite.QueueDeployment(ctx, next, tt.main); err != nil {
					t.Fatalf("replace rejected reservation: %v", err)
				}
			}
		})
	}
}

func TestQueuedDeploymentWorkerResolverErrorDoesNotStarveQueuedApp(t *testing.T) {
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	reservePendingRecovery(t, sqlite, "my-app", "dep_orphan", "broken")
	if err := sqlite.CreateApp(ctx, app.App{ID: "other-app", Name: "other-app", NodeID: "local", RepoPath: "/apps/other-app/repo.git", WorktreePath: "/apps/other-app/worktree", Status: app.AppStatusCreated}); err != nil {
		t.Fatal(err)
	}
	deployment := reservePendingRecovery(t, sqlite, "other-app", "dep_queued", "valid")
	if err := sqlite.RecordDeploymentQueued(ctx,
		app.Event{ID: EventID(deployment.ID, "git_ref_accepted"), AppID: deployment.AppID, Type: "git.ref_accepted"},
		app.Event{ID: EventID(deployment.ID, "queued"), AppID: deployment.AppID, Type: "deploy.queued"},
	); err != nil {
		t.Fatal(err)
	}
	executor := &recordingQueuedDeploymentExecutor{}
	worker := NewQueuedDeploymentWorker(QueuedDeploymentWorkerConfig{Store: sqlite, Executor: executor, LocksDir: t.TempDir(),
		CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) { return "", errors.New("unreadable main") }),
	})
	worked, err := worker.RunOne(ctx)
	if !worked || executor.deployment.ID != deployment.ID {
		t.Fatalf("RunOne = %v, %v; executed %#v, want other queued app", worked, err, executor.deployment)
	}
}

func reservePendingRecovery(t *testing.T, sqlite *store.SQLiteStore, appID, id, sha string) app.Deployment {
	t.Helper()
	deployment := app.Deployment{ID: id, AppID: appID, ReleaseID: app.ReleaseID(appID, sha), CommitSHA: sha, Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	if err := sqlite.QueueDeployment(context.Background(), deployment, ""); err != nil {
		t.Fatal(err)
	}
	return deployment
}

func TestQueuedDeploymentWorkerRecoversOlderReservationBeforeNewerQueuedApp(t *testing.T) {
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	older := reservePendingRecovery(t, sqlite, "my-app", "dep_older", "abc123")
	if err := sqlite.CreateApp(ctx, app.App{ID: "other-app", Name: "other-app", NodeID: "local", RepoPath: "/apps/other-app/repo.git", Status: app.AppStatusCreated}); err != nil {
		t.Fatal(err)
	}
	newer := app.Deployment{ID: "dep_newer", AppID: "other-app", ReleaseID: app.ReleaseID("other-app", "def456"), CommitSHA: "def456", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: older.StartedAt.Add(time.Second)}
	if err := sqlite.QueueDeployment(ctx, newer, ""); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.RecordDeploymentQueued(ctx,
		app.Event{ID: EventID(newer.ID, "git_ref_accepted"), AppID: newer.AppID, Type: "git.ref_accepted"},
		app.Event{ID: EventID(newer.ID, "queued"), AppID: newer.AppID, Type: "deploy.queued"},
	); err != nil {
		t.Fatal(err)
	}
	executor := &recordingQueuedDeploymentExecutor{}
	worker := NewQueuedDeploymentWorker(QueuedDeploymentWorkerConfig{Store: sqlite, Executor: executor, LocksDir: t.TempDir(),
		CurrentMainResolver: app.CurrentMainResolverFunc(func(context.Context, string) (string, error) { return "abc123", nil }),
	})
	worked, err := worker.RunOne(ctx)
	if err != nil || !worked || executor.deployment.ID != older.ID {
		t.Fatalf("RunOne = %v, %v; executed %#v, want older accepted reservation", worked, err, executor.deployment)
	}
}

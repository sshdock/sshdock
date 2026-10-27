package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestQueueRedeploymentSharesAdmissionAndLogsWithPush(t *testing.T) {
	for _, active := range []app.DeploymentStatus{"", app.DeploymentStatusPending, app.DeploymentStatusDeploying} {
		t.Run(string(active), func(t *testing.T) {
			ctx := context.Background()
			db := newTestStore(t, ctx)
			now := time.Now().UTC()
			if err := db.CreateApp(ctx, app.App{ID: "my-app", Name: "my-app", NodeID: "local", Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			if active != "" {
				previous := app.Deployment{ID: "dep_push", AppID: "my-app", ReleaseID: "rel_push", CommitSHA: "push", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now}
				if err := db.QueueDeployment(ctx, previous, "old"); err != nil {
					t.Fatal(err)
				}
				if active == app.DeploymentStatusDeploying {
					if err := db.UpdateDeploymentStatus(ctx, previous.ID, active, time.Time{}, ""); err != nil {
						t.Fatal(err)
					}
				}
			}
			attempt := app.Deployment{ID: "dep_retry", AppID: "my-app", ReleaseID: "rel_main", CommitSHA: "main", Trigger: app.DeploymentTriggerRedeploy, Status: app.DeploymentStatusPending, StartedAt: now}
			err := db.QueueRedeployment(ctx, attempt)
			if active != "" {
				if !errors.Is(err, ErrActiveDeployment) {
					t.Fatalf("QueueRedeployment = %v, want busy", err)
				}
				if _, err := db.DeploymentLog(ctx, "my-app", attempt.ID); !errors.Is(err, ErrNotFound) {
					t.Fatalf("rejected attempt log = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.DeploymentLog(ctx, "my-app", attempt.ID); err != nil {
				t.Fatal(err)
			}
			model, err := db.GetApp(ctx, "my-app")
			if err != nil || model.Status != app.AppStatusDeploying {
				t.Fatalf("app = %#v, error = %v", model, err)
			}
			push := app.Deployment{ID: "dep_new_push", AppID: "my-app", CommitSHA: "new", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now}
			if err := db.QueueDeployment(ctx, push, "different-main"); !errors.Is(err, ErrActiveDeployment) {
				t.Fatalf("push during redeploy = %v, want busy", err)
			}
		})
	}
}

func TestQueueRedeploymentRollsBackForMissingApp(t *testing.T) {
	ctx := context.Background()
	db := newTestStore(t, ctx)
	attempt := app.Deployment{ID: "dep_missing", AppID: "missing", Trigger: app.DeploymentTriggerRedeploy, Status: app.DeploymentStatusPending, StartedAt: time.Now()}
	if err := db.QueueRedeployment(ctx, attempt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("QueueRedeployment = %v", err)
	}
	rows, err := db.ListDeploymentsByApp(ctx, "missing")
	if err != nil || len(rows) != 0 {
		t.Fatalf("attempts = %#v, error = %v", rows, err)
	}
	if _, err := db.DeploymentLog(ctx, "missing", attempt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back log = %v", err)
	}
}

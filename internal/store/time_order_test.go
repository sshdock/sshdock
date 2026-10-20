package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestSQLiteHistoryOrdersVariablePrecisionUTCTimestamps(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, ctx)
	base := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	// RFC3339Nano writes these as ...00Z, ...00.1Z, ...00.100000001Z.
	// Sorting the literal strings reverses chronological order at each prefix.
	for i, offset := range []time.Duration{0, 100 * time.Millisecond, 100*time.Millisecond + time.Nanosecond} {
		id, at := fmt.Sprint(i), base.Add(offset)
		if err := s.CreateRelease(ctx, app.Release{ID: id, AppID: "app", CommitSHA: id, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateEvent(ctx, app.Event{ID: id, AppID: "app", Type: "deploy.started", CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateDeployment(ctx, app.Deployment{ID: id, AppID: "app", ReleaseID: "0", CommitSHA: "0", Status: app.DeploymentStatusSucceeded, StartedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	releases, err := s.ListReleasesByApp(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.ListEventsByApp(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	deployments, err := s.ListDeploymentsByApp(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	byStatus, err := s.ListDeploymentsByStatus(ctx, app.DeploymentStatusSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		id := fmt.Sprint(i)
		if releases[i].ID != id || events[i].ID != id || deployments[i].ID != id || byStatus[i].ID != id {
			t.Fatalf("history position %d: release=%s event=%s deployment=%s byStatus=%s", i, releases[i].ID, events[i].ID, deployments[i].ID, byStatus[i].ID)
		}
	}
	latest, err := s.FindDeploymentByAppCommit(ctx, "app", "0")
	if err != nil || latest.ID != "2" {
		t.Fatalf("latest attempt=%s err=%v, want 2", latest.ID, err)
	}
}

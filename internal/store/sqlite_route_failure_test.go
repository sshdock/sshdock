package store

import (
	"context"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestSQLiteStoreRouteApplyFailuresFollowAppLifecycle(t *testing.T) {
	ctx := context.Background()
	sqlite := newTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	model := app.App{
		ID: "my-app", Name: "my-app", NodeID: "local", Status: app.AppStatusCreated,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := sqlite.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	failure := RouteApplyFailure{
		AppID: "my-app", ServiceName: "web", DomainName: "my-app.example.com",
		Port: 3000, HTTPS: true, Operation: RouteApplyAttach,
		Detail: "reload failed", UpdatedAt: now,
	}
	if err := sqlite.UpsertRouteApplyFailure(ctx, failure); err != nil {
		t.Fatalf("UpsertRouteApplyFailure: %v", err)
	}
	failure.Operation = RouteApplyDetach
	failure.Detail = "detach reload failed"
	failure.UpdatedAt = now.Add(time.Minute)
	if err := sqlite.UpsertRouteApplyFailure(ctx, failure); err != nil {
		t.Fatalf("UpsertRouteApplyFailure update: %v", err)
	}

	failures, err := sqlite.ListRouteApplyFailuresByApp(ctx, model.ID)
	if err != nil {
		t.Fatalf("ListRouteApplyFailuresByApp: %v", err)
	}
	if len(failures) != 1 || failures[0] != failure {
		t.Fatalf("route apply failures = %#v, want [%#v]", failures, failure)
	}
	if err := sqlite.DeleteApp(ctx, model.ID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	failures, err = sqlite.ListRouteApplyFailuresByApp(ctx, model.ID)
	if err != nil {
		t.Fatalf("ListRouteApplyFailuresByApp after delete: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("route apply failures after app delete = %#v, want none", failures)
	}
}

func TestSQLiteStoreClearRouteApplyFailures(t *testing.T) {
	ctx := context.Background()
	sqlite := newTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	failure := RouteApplyFailure{
		AppID: "my-app", ServiceName: "web", DomainName: "my-app.example.com",
		Port: 3000, HTTPS: true, Operation: RouteApplyAttach,
		Detail: "reload failed", UpdatedAt: now,
	}
	if err := sqlite.UpsertRouteApplyFailure(ctx, failure); err != nil {
		t.Fatalf("UpsertRouteApplyFailure: %v", err)
	}
	if err := sqlite.ClearRouteApplyFailures(ctx); err != nil {
		t.Fatalf("ClearRouteApplyFailures: %v", err)
	}
	failures, err := sqlite.ListRouteApplyFailuresByApp(ctx, failure.AppID)
	if err != nil {
		t.Fatalf("ListRouteApplyFailuresByApp: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("route apply failures after clear = %#v, want none", failures)
	}
}

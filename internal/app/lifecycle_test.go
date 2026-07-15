package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestServiceStartAndStopUseLatestSuccessfulReleaseAndRecordEvents(t *testing.T) {
	// Given
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	store := newFakeServiceStore()
	store.apps["app_1"] = App{ID: "app_1", WorktreePath: "/apps/app_1/worktree"}
	store.releases["rel_1"] = Release{ID: "rel_1", AppID: "app_1", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}
	runner := &compose.FakeRunner{}
	resolver := &fakeConfigResolver{env: map[string]string{"APP_MESSAGE": "configured"}}
	service := NewService(store, WithClock(func() time.Time { return now }), WithRecoveryRunner(runner), WithConfigResolver(resolver))

	// When
	if err := service.StopApp(ctx, "app_1"); err != nil {
		t.Fatalf("StopApp: %v", err)
	}
	if err := service.StartApp(ctx, "app_1"); err != nil {
		t.Fatalf("StartApp: %v", err)
	}

	// Then
	wantRequest := compose.LifecycleRequest{AppName: "app_1", ProjectDir: "/apps/app_1/worktree", ComposePath: "/apps/app_1/worktree/compose.yml", Env: resolver.env}
	if len(runner.StopRequests) != 1 || !reflect.DeepEqual(runner.StopRequests[0], wantRequest) {
		t.Fatalf("StopRequests = %#v, want %#v", runner.StopRequests, wantRequest)
	}
	if len(runner.StartRequests) != 1 || !reflect.DeepEqual(runner.StartRequests[0], wantRequest) {
		t.Fatalf("StartRequests = %#v, want %#v", runner.StartRequests, wantRequest)
	}
	assertEventTypes(t, store.events["app_1"], []string{"stop.started", "stop.succeeded", "start.started", "start.succeeded"})
}

func TestServiceStartFailureRecordsActionableRedeployGuidance(t *testing.T) {
	// Given
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	store := newFakeServiceStore()
	store.apps["app_1"] = App{ID: "app_1", WorktreePath: "/apps/app_1/worktree"}
	store.releases["rel_1"] = Release{ID: "rel_1", AppID: "app_1", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}
	failure := errors.New("no containers to start")
	runner := &compose.FakeRunner{StartErr: failure}
	service := NewService(store, WithClock(func() time.Time { return now }), WithRecoveryRunner(runner))

	// When
	err := service.StartApp(ctx, "app_1")

	// Then
	if !errors.Is(err, failure) {
		t.Fatalf("StartApp error = %v, want wrapped %v", err, failure)
	}
	if !strings.Contains(err.Error(), "sudo sshdock apps redeploy app_1") {
		t.Fatalf("StartApp error = %q, want redeploy guidance", err)
	}
	events := store.events["app_1"]
	assertEventTypes(t, events, []string{"start.started", "start.failed"})
	if !strings.Contains(events[1].Message, "sudo sshdock apps redeploy app_1") {
		t.Fatalf("start.failed message = %q, want redeploy guidance", events[1].Message)
	}
}

func TestServiceRestartPassesResolvedConfigEnvironment(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	store := newFakeServiceStore()
	store.apps["app_1"] = App{ID: "app_1", WorktreePath: "/apps/app_1/worktree"}
	store.releases["rel_1"] = Release{ID: "rel_1", AppID: "app_1", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}
	runner := &compose.FakeRunner{}
	resolver := &fakeConfigResolver{env: map[string]string{"APP_MESSAGE": "configured"}}
	service := NewService(store, WithClock(func() time.Time { return now }), WithRecoveryRunner(runner), WithConfigResolver(resolver))

	if err := service.RestartApp(ctx, "app_1"); err != nil {
		t.Fatalf("RestartApp: %v", err)
	}
	if len(runner.RestartRequests) != 1 || runner.RestartRequests[0].Env["APP_MESSAGE"] != "configured" {
		t.Fatalf("RestartRequests = %#v, want stored config environment", runner.RestartRequests)
	}
}

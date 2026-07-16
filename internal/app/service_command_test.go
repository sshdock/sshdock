package app

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestServiceExecAndRunUseCurrentWorktreeComposeEntryAndRecordEvents(t *testing.T) {
	// Given
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	worktreePath, composePath := writeCurrentCompose(t)
	store := newFakeServiceStore()
	store.apps["app_1"] = App{ID: "app_1", WorktreePath: worktreePath}
	store.releases["rel_1"] = Release{ID: "rel_1", AppID: "app_1", ComposePath: "/historical/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}
	runner := &compose.FakeRunner{}
	resolver := &fakeConfigResolver{env: map[string]string{"APP_MESSAGE": "configured"}}
	service := NewService(store, WithClock(func() time.Time { return now }), WithRecoveryRunner(runner), WithConfigResolver(resolver))
	var stdout bytes.Buffer
	request := compose.ServiceCommandRequest{AppName: "app_1", ServiceName: "web", Command: []string{"printf", "%s", "value with spaces"}, TTY: true, Stdout: &stdout}

	// When
	if err := service.ExecApp(ctx, request); err != nil {
		t.Fatalf("ExecApp: %v", err)
	}
	request.TTY = false
	if err := service.RunApp(ctx, request); err != nil {
		t.Fatalf("RunApp: %v", err)
	}

	// Then
	wantRequest := request
	wantRequest.ProjectDir = worktreePath
	wantRequest.ComposePath = composePath
	wantRequest.Env = resolver.env
	if len(runner.ExecRequests) != 1 {
		t.Fatalf("ExecRequests = %#v", runner.ExecRequests)
	}
	wantExec := wantRequest
	wantExec.TTY = true
	if !reflect.DeepEqual(runner.ExecRequests[0], wantExec) {
		t.Fatalf("ExecRequests[0] = %#v, want %#v", runner.ExecRequests[0], wantExec)
	}
	if len(runner.RunRequests) != 1 || !reflect.DeepEqual(runner.RunRequests[0], wantRequest) {
		t.Fatalf("RunRequests = %#v, want %#v", runner.RunRequests, wantRequest)
	}
	assertEventTypes(t, store.events["app_1"], []string{"exec.started", "exec.succeeded", "run.started", "run.succeeded"})
}

func TestServiceCommandFailureIsRedactedInErrorAndEvent(t *testing.T) {
	// Given
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	secret := "stored-secret"
	store := newFakeServiceStore()
	worktreePath, _ := writeCurrentCompose(t)
	store.apps["app_1"] = App{ID: "app_1", WorktreePath: worktreePath}
	store.releases["rel_1"] = Release{ID: "rel_1", AppID: "app_1", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}
	runner := &compose.FakeRunner{ExecErr: errors.New("container failed with " + secret)}
	resolver := allValueConfigResolver{env: map[string]string{"TOKEN": secret}, redactionValues: map[string]string{"TOKEN": secret}}
	service := NewService(store, WithClock(func() time.Time { return now }), WithRecoveryRunner(runner), WithConfigResolver(resolver))

	// When
	err := service.ExecApp(ctx, compose.ServiceCommandRequest{AppName: "app_1", ServiceName: "web", Command: []string{"false"}})

	// Then
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("ExecApp error = %v", err)
	}
	events := store.events["app_1"]
	assertEventTypes(t, events, []string{"exec.started", "exec.failed"})
	if events[1].Message != "Exec failed for service app_1/web" {
		t.Fatalf("failure event message = %q", events[1].Message)
	}
}

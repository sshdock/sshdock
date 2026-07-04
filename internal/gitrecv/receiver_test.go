package gitrecv

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestBareRepoPath(t *testing.T) {
	got := BareRepoPath("/var/lib/sshdock/apps", "my-app")
	want := filepath.Join("/var/lib/sshdock/apps", "my-app", "repo.git")

	if got != want {
		t.Fatalf("BareRepoPath = %q, want %q", got, want)
	}
}

func TestParsePostReceiveLineCreatesPushEvent(t *testing.T) {
	event, err := ParsePostReceiveLine("my-app", "/data/apps/my-app/repo.git", "oldsha abc123 refs/heads/main")
	if err != nil {
		t.Fatalf("ParsePostReceiveLine: %v", err)
	}

	if event.AppName != "my-app" {
		t.Fatalf("AppName = %q", event.AppName)
	}
	if event.RepoPath != "/data/apps/my-app/repo.git" {
		t.Fatalf("RepoPath = %q", event.RepoPath)
	}
	if event.Branch != "main" {
		t.Fatalf("Branch = %q", event.Branch)
	}
	if event.CommitSHA != "abc123" {
		t.Fatalf("CommitSHA = %q", event.CommitSHA)
	}
}

func TestParsePostReceiveLineRejectsInvalidInput(t *testing.T) {
	_, err := ParsePostReceiveLine("my-app", "/repo.git", "not enough fields")
	if err == nil {
		t.Fatal("ParsePostReceiveLine error = nil, want error")
	}
}

func TestReceiverTriggersDeployCallback(t *testing.T) {
	ctx := context.Background()
	var got PushEvent
	receiver := NewReceiver(func(_ context.Context, event PushEvent) error {
		got = event
		return nil
	})
	event := PushEvent{
		AppName:   "my-app",
		RepoPath:  "/repo.git",
		Branch:    "main",
		CommitSHA: "abc123",
	}

	if err := receiver.HandlePush(ctx, event); err != nil {
		t.Fatalf("HandlePush: %v", err)
	}
	if got != event {
		t.Fatalf("callback event = %#v, want %#v", got, event)
	}
}

func TestReceiverReturnsDeployCallbackError(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("deploy failed")
	receiver := NewReceiver(func(_ context.Context, _ PushEvent) error {
		return failure
	})

	err := receiver.HandlePush(ctx, PushEvent{AppName: "my-app"})
	if !errors.Is(err, failure) {
		t.Fatalf("HandlePush error = %v, want %v", err, failure)
	}
}

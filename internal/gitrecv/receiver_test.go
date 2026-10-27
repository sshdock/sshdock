package gitrecv

import (
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

func TestParsePostReceiveLineRejectsNonMainDestination(t *testing.T) {
	// Given
	line := "oldsha abc123 refs/heads/feature"

	// When
	_, err := ParsePostReceiveLine("my-app", "/repo.git", line)

	// Then
	if err == nil {
		t.Fatal("ParsePostReceiveLine error = nil, want rejection")
	}
}

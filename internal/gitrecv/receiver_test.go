package gitrecv

import (
	"fmt"
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

func TestParsePostReceiveLineReturnsCommitSHA(t *testing.T) {
	commitSHA, err := ParsePostReceiveLine("oldsha abc123 refs/heads/main")
	if err != nil {
		t.Fatalf("ParsePostReceiveLine: %v", err)
	}

	if commitSHA != "abc123" {
		t.Fatalf("commit SHA = %q, want abc123", commitSHA)
	}
}

func TestParsePostReceiveLineRejectsInvalidInput(t *testing.T) {
	for _, line := range []string{"", "oldsha abc123", "oldsha abc123 refs/heads/main extra"} {
		t.Run(line, func(t *testing.T) {
			_, err := ParsePostReceiveLine(line)
			if err == nil || err.Error() != "post-receive line must contain old sha, new sha, and ref" {
				t.Fatalf("ParsePostReceiveLine error = %v, want invalid field count", err)
			}
		})
	}
}

func TestParsePostReceiveLineRejectsNonMainDestination(t *testing.T) {
	for _, ref := range []string{"refs/heads/feature", "refs/tags/main", "main"} {
		t.Run(ref, func(t *testing.T) {
			_, err := ParsePostReceiveLine("oldsha abc123 " + ref)
			want := fmt.Sprintf("unsupported destination %q: only remote refs/heads/main is deployable", ref)
			if err == nil || err.Error() != want {
				t.Fatalf("ParsePostReceiveLine error = %v, want %q", err, want)
			}
		})
	}
}

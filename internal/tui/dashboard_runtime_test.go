package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestDashboardSecretFallbackDoesNotDependOnCheckoutOrRedactPublicSHA(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "HEAD"), []byte("invalid-head"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	secret := "stored-secret"
	store := &fakeDashboardStore{
		apps:             []app.App{{ID: "my-app", Name: "my-app", RepoPath: repo, WorktreePath: worktree}},
		deploymentsByApp: map[string][]app.Deployment{"my-app": {{ID: "dep_failed", CommitSHA: sha, Status: app.DeploymentStatusFailed, ErrorMessage: sha + " " + secret}}},
		eventsByApp:      map[string][]app.Event{"my-app": {{ID: "evt_failed", Type: "deploy.failed", Message: sha + " " + secret}}},
	}
	resolver := dashboardSecretResolver{"TOKEN": secret}
	runner := &compose.FakeRunner{LogOutput: sha + " " + secret}
	health := &fakeDashboardHealthProvider{reports: map[string]app.HealthReport{"my-app": {}}}
	handler := NewDashboardHandlerWithConfig(store, runner, resolver, health)
	snapshot, err := handler.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("bad HEAD blocked secret-only dashboard snapshot: %v", err)
	}
	var rendered bytes.Buffer
	if err := RenderDashboardSnapshot(&rendered, snapshot); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.String(), secret) || !strings.Contains(rendered.String(), sha) || !strings.Contains(rendered.String(), "<redacted>") {
		t.Fatalf("history redaction = %q", rendered.String())
	}
	if _, err := handler.Logs(context.Background(), "my-app", []ServiceView{{Name: "web"}}); err == nil || len(runner.LogsRequests) != 0 {
		t.Fatalf("invalid checkout logs error = %v, requests = %#v", err, runner.LogsRequests)
	}
	if err := os.WriteFile(filepath.Join(repo, "HEAD"), []byte(sha), 0o644); err != nil {
		t.Fatal(err)
	}
	logs, err := handler.Logs(context.Background(), "my-app", []ServiceView{{Name: "web"}})
	if err != nil || len(runner.LogsRequests) != 1 || runner.LogsRequests[0].Env[compose.GitSHAEnv] != sha {
		t.Fatalf("runtime logs = %#v, %v, requests = %#v", logs, err, runner.LogsRequests)
	}
	if got := strings.Join(logs["web"].Lines, "\n"); got != sha+" <redacted>" {
		t.Fatalf("logs = %q", got)
	}
	if _, injected := resolver[compose.GitSHAEnv]; injected {
		t.Fatal("runtime interpolation mutated the secret-only resolver")
	}
}

// This resolver deliberately provides no optional RedactionValues method.
type dashboardSecretResolver map[string]string

func (r dashboardSecretResolver) ResolveEnv(context.Context, string) (map[string]string, error) {
	return r, nil
}

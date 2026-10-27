package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestResolveRuntimeEnvUsesCheckoutInsteadOfPendingMain(t *testing.T) {
	repoPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoPath, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "refs", "heads", "main"), []byte(strings.Repeat("b", 40)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := &fakeConfigResolver{env: map[string]string{"TOKEN": "secret-value"}}
	model := App{ID: "my-app", RepoPath: repoPath}
	for _, test := range []struct {
		name string
		head string
		want string
	}{
		{name: "pending first push", head: "ref: refs/heads/main\n"},
		{name: "previous checkout while main is pending", head: strings.Repeat("a", 40) + "\n", want: strings.Repeat("a", 40)},
		{name: "new checkout", head: strings.Repeat("b", 40) + "\n", want: strings.Repeat("b", 40)},
		{name: "sha256 checkout", head: strings.Repeat("C", 64) + "\n", want: strings.Repeat("c", 64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(repoPath, "HEAD"), []byte(test.head), 0o644); err != nil {
				t.Fatal(err)
			}
			env, err := ResolveRuntimeEnv(context.Background(), model, resolver)
			if err != nil || env[compose.GitSHAEnv] != test.want || env["TOKEN"] != "secret-value" {
				t.Fatalf("runtime environment = %#v, error = %v", env, err)
			}
			if _, injected := resolver.env[compose.GitSHAEnv]; injected {
				t.Fatalf("runtime SHA mutated stored secret map: %#v", resolver.env)
			}
		})
	}
	if err := os.WriteFile(filepath.Join(repoPath, "HEAD"), []byte("not-a-commit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveRuntimeEnv(context.Background(), model, resolver); err == nil || !strings.Contains(err.Error(), "redeploy current remote main") {
		t.Fatalf("corrupt HEAD error = %v", err)
	}
	failure := errors.New("config unavailable")
	resolver.err = failure
	if _, err := ResolveRuntimeEnv(context.Background(), model, resolver); !errors.Is(err, failure) {
		t.Fatalf("config error = %v, want wrapped %v", err, failure)
	}
}

func TestResolveRuntimeEnvWithoutStoredConfigStillUsesCheckout(t *testing.T) {
	repoPath := t.TempDir()
	sha := strings.Repeat("a", 40)
	if err := os.WriteFile(filepath.Join(repoPath, "HEAD"), []byte(sha), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := ResolveRuntimeEnv(context.Background(), App{ID: "my-app", RepoPath: repoPath}, nil)
	if err != nil || env[compose.GitSHAEnv] != sha {
		t.Fatalf("runtime environment = %#v, error = %v", env, err)
	}
}

func TestCurrentComposeEntryRequiresManagedWorktree(t *testing.T) {
	worktree, composePath := writeCurrentCompose(t)
	if _, _, err := CurrentComposeEntry(App{ID: "my-app", ComposePath: composePath}); err == nil || !strings.Contains(err.Error(), "worktree path is empty") {
		t.Fatalf("stored Compose path fallback error = %v", err)
	}
	projectDir, entry, err := CurrentComposeEntry(App{ID: "my-app", WorktreePath: worktree, ComposePath: "/historical/compose.yml"})
	if err != nil || projectDir != worktree || entry != composePath {
		t.Fatalf("current Compose entry = %q, %q, %v", projectDir, entry, err)
	}
}

func TestRuntimeCommandsRecordConfigAndRevisionFailuresAfterStart(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*Service) error
	}{
		{name: "start", run: func(s *Service) error { return s.StartApp(context.Background(), "my-app") }},
		{name: "stop", run: func(s *Service) error { return s.StopApp(context.Background(), "my-app") }},
		{name: "restart", run: func(s *Service) error { return s.RestartApp(context.Background(), "my-app") }},
		{name: "exec", run: func(s *Service) error {
			return s.ExecApp(context.Background(), compose.ServiceCommandRequest{AppName: "my-app", ServiceName: "web", Command: []string{"true"}})
		}},
		{name: "run", run: func(s *Service) error {
			return s.RunApp(context.Background(), compose.ServiceCommandRequest{AppName: "my-app", ServiceName: "web", Command: []string{"true"}})
		}},
	} {
		for _, source := range []string{"config", "revision"} {
			t.Run(operation.name+"/"+source, func(t *testing.T) {
				repoPath := t.TempDir()
				if err := os.WriteFile(filepath.Join(repoPath, "HEAD"), []byte("invalid-head"), 0o644); err != nil {
					t.Fatal(err)
				}
				store := newFakeServiceStore()
				store.apps["my-app"] = App{ID: "my-app", RepoPath: repoPath, WorktreePath: testComposeWorktree(t)}
				resolver := &fakeConfigResolver{}
				if source == "config" {
					resolver.err = errors.New("config unavailable")
				}
				runner := &compose.FakeRunner{}
				service := NewService(store, WithRecoveryRunner(runner), WithConfigResolver(resolver))
				err := operation.run(service)
				if err == nil || (source == "config" && !errors.Is(err, resolver.err)) || (source == "revision" && !strings.Contains(err.Error(), "repository HEAD")) {
					t.Fatalf("operation error = %v", err)
				}
				assertEventTypes(t, store.events["my-app"], []string{operation.name + ".started", operation.name + ".failed"})
				if len(runner.StartRequests)+len(runner.StopRequests)+len(runner.RestartRequests)+len(runner.ExecRequests)+len(runner.RunRequests) != 0 {
					t.Fatal("runtime command ran despite invalid environment")
				}
			})
		}
	}
}

func writeRuntimeRepository(t *testing.T, sha string) string {
	t.Helper()
	repoPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoPath, "HEAD"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repoPath
}

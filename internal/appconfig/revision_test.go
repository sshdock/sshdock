package appconfig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestRuntimeEnvironmentUsesCheckoutInsteadOfPendingMain(t *testing.T) {
	ctx := context.Background()
	sqlite := newConfigTestStore(t, ctx)
	repoPath := t.TempDir()
	now := time.Now().UTC()
	if err := sqlite.CreateApp(ctx, app.App{ID: "external-app", Name: "external-app", NodeID: "local", RepoPath: repoPath, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	service := NewService(sqlite, filepath.Join(t.TempDir(), "config.key"))
	if err := service.Set(ctx, SetRequest{AppID: "external-app", Name: "TOKEN", Value: []byte("secret-value")}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoPath, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "refs", "heads", "main"), []byte(strings.Repeat("b", 40)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		head string
		want string
	}{
		{name: "pending first push", head: "ref: refs/heads/main\n"},
		{name: "previous checkout while new main is pending", head: strings.Repeat("a", 40) + "\n", want: strings.Repeat("a", 40)},
		{name: "new checkout", head: strings.Repeat("b", 40) + "\n", want: strings.Repeat("b", 40)},
		{name: "sha256 checkout", head: strings.Repeat("c", 64) + "\n", want: strings.Repeat("c", 64)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(repoPath, "HEAD"), []byte(test.head), 0o644); err != nil {
				t.Fatal(err)
			}
			env, err := service.ResolveAppConfig(ctx, "external-app")
			if err != nil || env[compose.GitSHAEnv] != test.want || env["TOKEN"] != "secret-value" {
				t.Fatalf("runtime environment = %#v, error = %v", env, err)
			}
		})
	}
	redacted, err := service.RedactionValues(ctx, "external-app")
	if err != nil || len(redacted) != 1 || redacted["external-app/TOKEN"] != "secret-value" {
		t.Fatalf("redaction values = %#v, error = %v", redacted, err)
	}
	entries, err := service.List(ctx, "external-app")
	if err != nil || len(entries) != 1 || entries[0].Name != "TOKEN" {
		t.Fatalf("stored config = %#v, error = %v", entries, err)
	}

	if err := os.WriteFile(filepath.Join(repoPath, "HEAD"), []byte("not-a-commit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveAppConfig(ctx, "external-app"); err == nil || !strings.Contains(err.Error(), "redeploy current remote main") {
		t.Fatalf("corrupt HEAD error = %v", err)
	}
}

package appconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestServiceSetListRevealUnsetAndImport(t *testing.T) {
	ctx := context.Background()
	sqlite := newConfigTestStore(t, ctx)
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	createConfigTestApp(t, ctx, sqlite, "my-app", now)
	service := NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), WithClock(func() time.Time { return now }))

	if err := service.Set(ctx, SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte("postgres://secret"), MutatedBy: "dashboard"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := service.Import(ctx, ImportRequest{
		AppID:     "my-app",
		Scope:     "worker",
		Input:     strings.NewReader("API_TOKEN=worker-secret\n# comment\nEMPTY=\n"),
		MutatedBy: "dashboard",
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	entries, err := service.List(ctx, "my-app")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %#v, want three", entries)
	}
	if entries[0].Name != "DATABASE_URL" || entries[0].Status != "set" || entries[0].RedactedValue != "<redacted>" {
		t.Fatalf("first entry = %#v", entries[0])
	}
	if entries[0].Value != "" {
		t.Fatalf("list exposed value %q", entries[0].Value)
	}

	value, err := service.Reveal(ctx, ConfigRef{AppID: "my-app", Name: "DATABASE_URL"})
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if value != "postgres://secret" {
		t.Fatalf("Reveal = %q", value)
	}

	stored, err := sqlite.GetAppConfigValue(ctx, store.AppConfigRef{AppID: "my-app", Name: "DATABASE_URL"})
	if err != nil {
		t.Fatalf("GetAppConfigValue: %v", err)
	}
	if strings.Contains(string(stored.Ciphertext), "secret") {
		t.Fatalf("ciphertext contains secret: %q", stored.Ciphertext)
	}

	if err := service.Unset(ctx, ConfigRef{AppID: "my-app", Name: "DATABASE_URL"}); err != nil {
		t.Fatalf("Unset: %v", err)
	}
	if _, err := service.Reveal(ctx, ConfigRef{AppID: "my-app", Name: "DATABASE_URL"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Reveal unset error = %v, want ErrNotFound", err)
	}
}

func TestServiceRequiresExistingAppBeforeStoringValues(t *testing.T) {
	ctx := context.Background()
	sqlite := newConfigTestStore(t, ctx)
	service := NewService(sqlite, filepath.Join(t.TempDir(), "config.key"))

	err := service.Set(ctx, SetRequest{AppID: "typo-app", Name: "SECRET", Value: []byte("secret")})
	if err == nil || !strings.Contains(err.Error(), `app "typo-app" not found`) {
		t.Fatalf("Set missing app error = %v", err)
	}
}

func TestServiceRevealReportsMissingHostKey(t *testing.T) {
	ctx := context.Background()
	sqlite := newConfigTestStore(t, ctx)
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	createConfigTestApp(t, ctx, sqlite, "my-app", now)
	keyPath := filepath.Join(t.TempDir(), "config.key")
	service := NewService(sqlite, keyPath, WithClock(func() time.Time { return now }))
	if err := service.Set(ctx, SetRequest{AppID: "my-app", Name: "SECRET", Value: []byte("secret")}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("Remove key: %v", err)
	}

	_, err := service.Reveal(ctx, ConfigRef{AppID: "my-app", Name: "SECRET"})
	if err == nil || !strings.Contains(err.Error(), "read config encryption key") {
		t.Fatalf("Reveal missing key error = %v", err)
	}
}

func TestServiceResolveEnvChecksManifestRequirements(t *testing.T) {
	ctx := context.Background()
	sqlite := newConfigTestStore(t, ctx)
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	createConfigTestApp(t, ctx, sqlite, "my-app", now)
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".sshdock.yml"), []byte(`config:
  required:
    - DATABASE_URL
    - API_TOKEN
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	service := NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), WithRecoveryHost("sshdock.example.com"), WithClock(func() time.Time { return now }))
	if err := service.Set(ctx, SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte("postgres://secret")}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	_, err := service.ResolveEnv(ctx, ResolveRequest{AppID: "my-app", ProjectDir: projectDir})
	if err == nil {
		t.Fatal("ResolveEnv error = nil, want missing key error")
	}
	for _, want := range []string{
		"missing required config for my-app: API_TOKEN",
		"ssh dashboard@sshdock.example.com config set my-app API_TOKEN",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ResolveEnv error missing %q:\n%v", want, err)
		}
	}

	if err := service.Set(ctx, SetRequest{AppID: "my-app", Name: "API_TOKEN", Value: []byte("api-secret")}); err != nil {
		t.Fatalf("Set API_TOKEN: %v", err)
	}
	env, err := service.ResolveEnv(ctx, ResolveRequest{AppID: "my-app", ProjectDir: projectDir})
	if err != nil {
		t.Fatalf("ResolveEnv: %v", err)
	}
	if env["DATABASE_URL"] != "postgres://secret" || env["API_TOKEN"] != "api-secret" {
		t.Fatalf("env = %#v", env)
	}
}

func newConfigTestStore(t *testing.T, ctx context.Context) *store.SQLiteStore {
	t.Helper()
	sqlite, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlite.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	return sqlite
}

func createConfigTestApp(t *testing.T, ctx context.Context, sqlite *store.SQLiteStore, appID string, now time.Time) {
	t.Helper()
	if err := sqlite.CreateApp(ctx, appmodel.App{
		ID:           appID,
		Name:         appID,
		NodeID:       "local",
		RepoPath:     "/apps/" + appID + "/repo.git",
		WorktreePath: "/apps/" + appID + "/worktree",
		Status:       appmodel.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
}

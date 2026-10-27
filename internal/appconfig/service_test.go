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
	if count, err := service.Import(ctx, ImportRequest{
		AppID:     "my-app",
		Input:     strings.NewReader("API_TOKEN=worker-secret\n# comment\nEMPTY=\n"),
		MutatedBy: "dashboard",
	}); err != nil || count != 2 {
		t.Fatalf("Import = %d, %v, want two values", count, err)
	}

	entries, err := service.List(ctx, "my-app")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %#v, want three", entries)
	}
	if entries[0].Name != "API_TOKEN" || entries[0].Status != "set" || entries[0].RedactedValue != "<redacted>" {
		t.Fatalf("first entry = %#v", entries[0])
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

func TestServiceResolveEnvSuppliesAllFlatValues(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newConfigTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	createConfigTestApp(t, ctx, sqlite, "my-app", now)
	service := NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), WithClock(func() time.Time { return now }))
	if err := service.Set(ctx, SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte("postgres://secret")}); err != nil {
		t.Fatalf("Set flat config: %v", err)
	}
	if err := service.Set(ctx, SetRequest{AppID: "my-app", Name: "API_TOKEN", Value: []byte("api-secret")}); err != nil {
		t.Fatalf("Set API_TOKEN: %v", err)
	}

	// When
	env, err := service.ResolveEnv(ctx, "my-app")

	// Then
	if err != nil {
		t.Fatalf("ResolveEnv: %v", err)
	}
	if env["DATABASE_URL"] != "postgres://secret" {
		t.Fatalf("env = %#v, want flat DATABASE_URL", env)
	}
	if env["API_TOKEN"] != "api-secret" {
		t.Fatalf("env = %#v, want flat API_TOKEN", env)
	}
}

func TestServiceSetRejectsOperationalEnvironmentNames(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newConfigTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	createConfigTestApp(t, ctx, sqlite, "my-app", now)
	service := NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), WithClock(func() time.Time { return now }))
	reserved := []string{"SSHDOCK_GIT_SHA", "SSHDOCK_CONFIG_KEY_PATH", "COMPOSE_PROJECT_NAME", "DOCKER_HOST", "SSH_AUTH_SOCK", "LD_PRELOAD", "BUILDKIT_HOST", "BUILDX_CONFIG", "PATH", "HOME"}

	for _, name := range reserved {
		t.Run(name, func(t *testing.T) {
			// When
			err := service.Set(ctx, SetRequest{AppID: "my-app", Name: name, Value: []byte("must-not-be-stored")})

			// Then
			if err == nil || !strings.Contains(err.Error(), "reserved for SSHDock operations") {
				t.Fatalf("Set(%s) error = %v, want reserved-name error", name, err)
			}
		})
	}
}

func TestServiceRedactionValuesIncludesAllFlatValues(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newConfigTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	createConfigTestApp(t, ctx, sqlite, "my-app", now)
	service := NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), WithClock(func() time.Time { return now }))
	if err := service.Set(ctx, SetRequest{AppID: "my-app", Name: "TOKEN", Value: []byte("flat-secret")}); err != nil {
		t.Fatalf("Set flat config: %v", err)
	}
	if err := service.Set(ctx, SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte("database-secret")}); err != nil {
		t.Fatalf("Set DATABASE_URL: %v", err)
	}

	// When
	values, err := service.RedactionValues(ctx, "my-app")

	// Then
	if err != nil {
		t.Fatalf("RedactionValues: %v", err)
	}
	got := map[string]bool{}
	for _, value := range values {
		got[value] = true
	}
	if !got["flat-secret"] || !got["database-secret"] || len(got) != 2 {
		t.Fatalf("redaction values = %#v", values)
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

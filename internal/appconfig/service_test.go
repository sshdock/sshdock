package appconfig

import (
	"bytes"
	"context"
	"database/sql"
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
	if entries[0].Name != "API_TOKEN" || entries[0].Status != "set" || entries[0].RedactedValue != "<redacted>" {
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

func TestServiceResolveEnvIgnoresLegacyManifestRequirements(t *testing.T) {
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

	service := NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), WithClock(func() time.Time { return now }))
	if err := service.Set(ctx, SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte("postgres://secret")}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	env, err := service.ResolveEnv(ctx, "my-app")
	if err != nil {
		t.Fatalf("ResolveEnv: %v", err)
	}
	if env["DATABASE_URL"] != "postgres://secret" || len(env) != 1 {
		t.Fatalf("env = %#v", env)
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

func TestServiceRevealsMigratedLegacyScopedValueWithExistingHostKey(t *testing.T) {
	// Given
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")
	keyPath := filepath.Join(dir, "config.key")
	key, err := LoadOrCreateHostKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateHostKey: %v", err)
	}
	ref := ConfigRef{AppID: "my-app", Name: "API_TOKEN"}
	aead, err := newAEAD(key)
	if err != nil {
		t.Fatalf("newAEAD: %v", err)
	}
	nonce := bytes.Repeat([]byte{0x03}, aead.NonceSize())
	legacyCiphertext := aead.Seal(nil, nonce, []byte("legacy-secret"), additionalData(ref, "worker", currentKeyVersion))

	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{query: `create table apps (id text primary key, name text not null, node_id text not null, repo_path text not null, worktree_path text not null, compose_path text not null, status text not null, created_at text not null, updated_at text not null)`},
		{query: `create table app_config_values (app_id text not null, name text not null, scope text not null default '', ciphertext blob not null, nonce blob not null, key_version integer not null, created_at text not null, updated_at text not null, mutated_by text not null, primary key (app_id, name, scope))`},
		{query: `insert into apps values ('my-app', 'my-app', 'local', '/apps/my-app/repo.git', '/apps/my-app/worktree', 'compose.yml', 'created', '2026-07-14T09:00:00Z', '2026-07-14T09:00:00Z')`},
		{query: `insert into app_config_values values ('my-app', 'API_TOKEN', 'worker', ?, ?, 1, '2026-07-14T09:00:00Z', '2026-07-14T09:00:00Z', 'dashboard')`, args: []any{legacyCiphertext, nonce}},
	}
	for _, statement := range statements {
		if _, err := legacy.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("prepare legacy database: %v\n%s", err, statement.query)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	// When
	sqlite, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlite.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	service := NewService(sqlite, keyPath)
	value, err := service.Reveal(ctx, ref)

	// Then
	if err != nil {
		t.Fatalf("Reveal migrated value: %v", err)
	}
	if value != "legacy-secret" {
		t.Fatalf("Reveal migrated value = %q", value)
	}
	database, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("ReadFile database: %v", err)
	}
	if bytes.Contains(database, []byte("legacy-secret")) {
		t.Fatal("migrated database contains plaintext config value")
	}
}

func TestServiceSetRejectsOperationalEnvironmentNames(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newConfigTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	createConfigTestApp(t, ctx, sqlite, "my-app", now)
	service := NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), WithClock(func() time.Time { return now }))
	reserved := []string{"SSHDOCK_CONFIG_KEY_PATH", "COMPOSE_PROJECT_NAME", "DOCKER_HOST", "SSH_AUTH_SOCK", "LD_PRELOAD", "BUILDKIT_HOST", "BUILDX_CONFIG", "PATH", "HOME"}

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

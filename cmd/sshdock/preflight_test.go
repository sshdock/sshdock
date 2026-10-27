package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestLocalPreflightDoesNotInitializeHostState(t *testing.T) {
	project := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), "absent")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	var stdout, stderr bytes.Buffer
	if code := runWithEnv([]string{"check", project, "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	var result struct {
		Data struct {
			OK    bool   `json:"ok"`
			Scope string `json:"scope"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.OK || result.Data.Scope != "local" || string(result.Error) != "null" || stderr.Len() != 0 {
		t.Fatalf("stdout=%s stderr=%s", &stdout, &stderr)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("local check initialized host state: %v", err)
	}
	entries, err := os.ReadDir(project)
	if err != nil || len(entries) != 0 {
		t.Fatalf("local check changed project: entries=%v err=%v", entries, err)
	}
}

func TestAppPreflightMissingStoreDoesNotCreateIt(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "absent")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(dataDir, "sshdock.db"))
	var stdout, stderr bytes.Buffer
	if code := runWithEnv([]string{"apps", "check", "missing", "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	var result struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error.Code != "unavailable" || stderr.Len() != 0 {
		t.Fatalf("stdout=%s stderr=%s", &stdout, &stderr)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("app check initialized host state: %v", err)
	}
}

func TestAppPreflightKeepsDatabaseAndMissingKeyUnchanged(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "sshdock.db")
	keyPath := filepath.Join(dataDir, "config.key")
	projectDir := filepath.Join(dataDir, "worktree")
	if err := os.Mkdir(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(projectDir, "compose.yml")
	if err := os.WriteFile(composePath, []byte("services:\n  web:\n    image: nginx:alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.CreateApp(ctx, app.App{ID: "read-only", Name: "read-only", WorktreePath: projectDir, ComposePath: composePath, Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", dbPath)
	t.Setenv("SSHDOCK_CONFIG_KEY_PATH", keyPath)
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := runWithEnv([]string{"apps", "check", "read-only", "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	var result struct {
		Data struct {
			OK     bool                            `json:"ok"`
			Checks []struct{ Name, Status string } `json:"checks"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.OK || string(result.Error) != "null" || stderr.Len() != 0 {
		t.Fatalf("stdout=%s stderr=%s", &stdout, &stderr)
	}
	nativeFailed := false
	for _, check := range result.Data.Checks {
		if check.Name == "native Compose validation" && check.Status == "fail" {
			nativeFailed = true
		}
	}
	if !nativeFailed {
		t.Fatalf("missing native Compose failure: %s", &stdout)
	}
	after, err := os.ReadFile(dbPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("preflight changed database: %v", err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("preflight created config key: %v", err)
	}
}

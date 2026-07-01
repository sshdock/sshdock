package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := Default()

	if cfg.DataDir == "" {
		t.Fatal("DataDir is empty")
	}
	if cfg.SQLiteDBPath != filepath.Join(cfg.DataDir, "rhumbase.db") {
		t.Fatalf("SQLiteDBPath = %q, want path under data dir", cfg.SQLiteDBPath)
	}
	if cfg.AppsDir != filepath.Join(cfg.DataDir, "apps") {
		t.Fatalf("AppsDir = %q, want path under data dir", cfg.AppsDir)
	}
	if cfg.NodeID != "local" {
		t.Fatalf("NodeID = %q, want local", cfg.NodeID)
	}
	if cfg.SSHListenAddr == "" {
		t.Fatal("SSHListenAddr is empty")
	}
	if cfg.DashboardUser == "" {
		t.Fatal("DashboardUser is empty")
	}
	if cfg.CaddyConfigPath == "" {
		t.Fatal("CaddyConfigPath is empty")
	}
}

func TestLoadFromEnvOverridesDefaults(t *testing.T) {
	t.Setenv("RHUMBASE_DATA_DIR", "/tmp/rhumbase-data")
	t.Setenv("RHUMBASE_SQLITE_DB_PATH", "/tmp/rhumbase.sqlite")
	t.Setenv("RHUMBASE_APPS_DIR", "/tmp/rhumbase-apps")
	t.Setenv("RHUMBASE_NODE_ID", "node-a")
	t.Setenv("RHUMBASE_SSH_LISTEN_ADDR", "127.0.0.1:2222")
	t.Setenv("RHUMBASE_DASHBOARD_USER", "operator")
	t.Setenv("RHUMBASE_CADDY_CONFIG_PATH", "/tmp/Caddyfile")

	cfg := LoadFromEnv()

	if cfg.DataDir != "/tmp/rhumbase-data" {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.SQLiteDBPath != "/tmp/rhumbase.sqlite" {
		t.Fatalf("SQLiteDBPath = %q", cfg.SQLiteDBPath)
	}
	if cfg.AppsDir != "/tmp/rhumbase-apps" {
		t.Fatalf("AppsDir = %q", cfg.AppsDir)
	}
	if cfg.NodeID != "node-a" {
		t.Fatalf("NodeID = %q", cfg.NodeID)
	}
	if cfg.SSHListenAddr != "127.0.0.1:2222" {
		t.Fatalf("SSHListenAddr = %q", cfg.SSHListenAddr)
	}
	if cfg.DashboardUser != "operator" {
		t.Fatalf("DashboardUser = %q", cfg.DashboardUser)
	}
	if cfg.CaddyConfigPath != "/tmp/Caddyfile" {
		t.Fatalf("CaddyConfigPath = %q", cfg.CaddyConfigPath)
	}
}

func TestAppPathsAreDerivedFromDataDirectory(t *testing.T) {
	t.Setenv("RHUMBASE_DATA_DIR", "/srv/rhumbase")
	t.Setenv("RHUMBASE_SQLITE_DB_PATH", "")
	t.Setenv("RHUMBASE_APPS_DIR", "")

	cfg := LoadFromEnv()

	if cfg.AppDir("my-app") != "/srv/rhumbase/apps/my-app" {
		t.Fatalf("AppDir = %q", cfg.AppDir("my-app"))
	}
	if cfg.AppRepoPath("my-app") != "/srv/rhumbase/apps/my-app/repo.git" {
		t.Fatalf("AppRepoPath = %q", cfg.AppRepoPath("my-app"))
	}
	if cfg.AppWorktreePath("my-app") != "/srv/rhumbase/apps/my-app/worktree" {
		t.Fatalf("AppWorktreePath = %q", cfg.AppWorktreePath("my-app"))
	}
}

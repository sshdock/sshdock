package config

import (
	"path/filepath"
	"strings"
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
	if cfg.GitUser != "git" {
		t.Fatalf("GitUser = %q, want git", cfg.GitUser)
	}
	if cfg.GitHomeDir != filepath.Join(cfg.DataDir, "git") {
		t.Fatalf("GitHomeDir = %q, want path under data dir", cfg.GitHomeDir)
	}
	if cfg.GitHost != "server" {
		t.Fatalf("GitHost = %q, want server", cfg.GitHost)
	}
	if cfg.GitAuthorizedKeysPath != filepath.Join(cfg.GitHomeDir, ".ssh", "authorized_keys") {
		t.Fatalf("GitAuthorizedKeysPath = %q, want path under git home", cfg.GitAuthorizedKeysPath)
	}
	if cfg.GitReceiveCommand != "sudo -n -u rhumbase /usr/local/bin/rhumbase-git-receive" {
		t.Fatalf("GitReceiveCommand = %q", cfg.GitReceiveCommand)
	}
	if cfg.DashboardHostKeyPath != filepath.Join(cfg.DataDir, "dashboard", "ssh_host_rsa_key") {
		t.Fatalf("DashboardHostKeyPath = %q, want path under dashboard data dir", cfg.DashboardHostKeyPath)
	}
	if cfg.DashboardAuthorizedKeysPath != filepath.Join(cfg.DataDir, "dashboard", ".ssh", "authorized_keys") {
		t.Fatalf("DashboardAuthorizedKeysPath = %q, want path under dashboard data dir", cfg.DashboardAuthorizedKeysPath)
	}
	if cfg.DashboardCommand != "sudo -n -u rhumbase /usr/local/bin/rhumbase-dashboard" {
		t.Fatalf("DashboardCommand = %q", cfg.DashboardCommand)
	}
	if cfg.CaddyConfigPath == "" {
		t.Fatal("CaddyConfigPath is empty")
	}
	if cfg.CaddyAdminAddress != "" {
		t.Fatalf("CaddyAdminAddress = %q, want empty default", cfg.CaddyAdminAddress)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate default config: %v", err)
	}
}

func TestLoadFromEnvOverridesDefaults(t *testing.T) {
	t.Setenv("RHUMBASE_DATA_DIR", "/tmp/rhumbase-data")
	t.Setenv("RHUMBASE_SQLITE_DB_PATH", "/tmp/rhumbase.sqlite")
	t.Setenv("RHUMBASE_APPS_DIR", "/tmp/rhumbase-apps")
	t.Setenv("RHUMBASE_NODE_ID", "node-a")
	t.Setenv("RHUMBASE_SSH_LISTEN_ADDR", "127.0.0.1:2222")
	t.Setenv("RHUMBASE_DASHBOARD_USER", "operator")
	t.Setenv("RHUMBASE_GIT_USER", "deploy")
	t.Setenv("RHUMBASE_GIT_HOME_DIR", "/tmp/rhumbase-git")
	t.Setenv("RHUMBASE_GIT_HOST", "rhumbase.example.com")
	t.Setenv("RHUMBASE_GIT_AUTHORIZED_KEYS_PATH", "/tmp/authorized_keys")
	t.Setenv("RHUMBASE_GIT_RECEIVE_COMMAND", "/opt/rhumbase/bin/rhumbased git-receive")
	t.Setenv("RHUMBASE_DASHBOARD_HOST_KEY_PATH", "/tmp/dashboard_host_key")
	t.Setenv("RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH", "/tmp/dashboard_authorized_keys")
	t.Setenv("RHUMBASE_DASHBOARD_COMMAND", "/opt/rhumbase/bin/rhumbased dashboard")
	t.Setenv("RHUMBASE_CADDY_CONFIG_PATH", "/tmp/Caddyfile")
	t.Setenv("RHUMBASE_CADDY_ADMIN_ADDRESS", "127.0.0.1:22019")

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
	if cfg.GitUser != "deploy" {
		t.Fatalf("GitUser = %q", cfg.GitUser)
	}
	if cfg.GitHomeDir != "/tmp/rhumbase-git" {
		t.Fatalf("GitHomeDir = %q", cfg.GitHomeDir)
	}
	if cfg.GitHost != "rhumbase.example.com" {
		t.Fatalf("GitHost = %q", cfg.GitHost)
	}
	if cfg.GitAuthorizedKeysPath != "/tmp/authorized_keys" {
		t.Fatalf("GitAuthorizedKeysPath = %q", cfg.GitAuthorizedKeysPath)
	}
	if cfg.GitReceiveCommand != "/opt/rhumbase/bin/rhumbased git-receive" {
		t.Fatalf("GitReceiveCommand = %q", cfg.GitReceiveCommand)
	}
	if cfg.DashboardHostKeyPath != "/tmp/dashboard_host_key" {
		t.Fatalf("DashboardHostKeyPath = %q", cfg.DashboardHostKeyPath)
	}
	if cfg.DashboardAuthorizedKeysPath != "/tmp/dashboard_authorized_keys" {
		t.Fatalf("DashboardAuthorizedKeysPath = %q", cfg.DashboardAuthorizedKeysPath)
	}
	if cfg.DashboardCommand != "/opt/rhumbase/bin/rhumbased dashboard" {
		t.Fatalf("DashboardCommand = %q", cfg.DashboardCommand)
	}
	if cfg.CaddyConfigPath != "/tmp/Caddyfile" {
		t.Fatalf("CaddyConfigPath = %q", cfg.CaddyConfigPath)
	}
	if cfg.CaddyAdminAddress != "127.0.0.1:22019" {
		t.Fatalf("CaddyAdminAddress = %q", cfg.CaddyAdminAddress)
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

func TestValidateReportsActionableMissingFields(t *testing.T) {
	cfg := Default()
	cfg.DataDir = " "
	cfg.GitHost = " "
	cfg.DashboardCommand = " "
	cfg.GitReceiveCommand = " "

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate error = nil, want missing field errors")
	}

	message := err.Error()
	for _, want := range []string{
		"RHUMBASE_DATA_DIR is required",
		"RHUMBASE_GIT_HOST is required",
		"RHUMBASE_DASHBOARD_COMMAND is required",
		"RHUMBASE_GIT_RECEIVE_COMMAND is required",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("Validate error missing %q:\n%s", want, message)
		}
	}
}

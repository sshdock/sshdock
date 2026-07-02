package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	DataDir                     string
	SQLiteDBPath                string
	AppsDir                     string
	NodeID                      string
	SSHListenAddr               string
	DashboardUser               string
	DashboardHostKeyPath        string
	DashboardAuthorizedKeysPath string
	GitUser                     string
	GitHomeDir                  string
	GitHost                     string
	GitAuthorizedKeysPath       string
	GitReceiveCommand           string
	CaddyConfigPath             string
	CaddyAdminAddress           string
}

func Default() Config {
	dataDir := "/var/lib/rhumbase"

	return Config{
		DataDir:                     dataDir,
		SQLiteDBPath:                filepath.Join(dataDir, "rhumbase.db"),
		AppsDir:                     filepath.Join(dataDir, "apps"),
		NodeID:                      "local",
		SSHListenAddr:               ":2222",
		DashboardUser:               "dashboard",
		DashboardHostKeyPath:        filepath.Join(dataDir, "dashboard", "ssh_host_rsa_key"),
		DashboardAuthorizedKeysPath: filepath.Join(dataDir, "dashboard", "authorized_keys"),
		GitUser:                     "git",
		GitHomeDir:                  filepath.Join(dataDir, "git"),
		GitHost:                     "server",
		GitAuthorizedKeysPath:       filepath.Join(dataDir, "git", ".ssh", "authorized_keys"),
		GitReceiveCommand:           "/usr/local/bin/rhumbased git-receive",
		CaddyConfigPath:             "/etc/caddy/rhumbase.caddyfile",
	}
}

func LoadFromEnv() Config {
	cfg := Default()

	cfg.DataDir = envOrDefault("RHUMBASE_DATA_DIR", cfg.DataDir)
	cfg.SQLiteDBPath = envOrDefault("RHUMBASE_SQLITE_DB_PATH", filepath.Join(cfg.DataDir, "rhumbase.db"))
	cfg.AppsDir = envOrDefault("RHUMBASE_APPS_DIR", filepath.Join(cfg.DataDir, "apps"))
	cfg.NodeID = envOrDefault("RHUMBASE_NODE_ID", cfg.NodeID)
	cfg.SSHListenAddr = envOrDefault("RHUMBASE_SSH_LISTEN_ADDR", cfg.SSHListenAddr)
	cfg.DashboardUser = envOrDefault("RHUMBASE_DASHBOARD_USER", cfg.DashboardUser)
	cfg.DashboardHostKeyPath = envOrDefault("RHUMBASE_DASHBOARD_HOST_KEY_PATH", filepath.Join(cfg.DataDir, "dashboard", "ssh_host_rsa_key"))
	cfg.DashboardAuthorizedKeysPath = envOrDefault("RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH", filepath.Join(cfg.DataDir, "dashboard", "authorized_keys"))
	cfg.GitUser = envOrDefault("RHUMBASE_GIT_USER", cfg.GitUser)
	cfg.GitHomeDir = envOrDefault("RHUMBASE_GIT_HOME_DIR", filepath.Join(cfg.DataDir, "git"))
	cfg.GitHost = envOrDefault("RHUMBASE_GIT_HOST", cfg.GitHost)
	cfg.GitAuthorizedKeysPath = envOrDefault("RHUMBASE_GIT_AUTHORIZED_KEYS_PATH", filepath.Join(cfg.GitHomeDir, ".ssh", "authorized_keys"))
	cfg.GitReceiveCommand = envOrDefault("RHUMBASE_GIT_RECEIVE_COMMAND", cfg.GitReceiveCommand)
	cfg.CaddyConfigPath = envOrDefault("RHUMBASE_CADDY_CONFIG_PATH", cfg.CaddyConfigPath)
	cfg.CaddyAdminAddress = envOrDefault("RHUMBASE_CADDY_ADMIN_ADDRESS", cfg.CaddyAdminAddress)

	return cfg
}

func (c Config) AppDir(appName string) string {
	return filepath.Join(c.AppsDir, appName)
}

func (c Config) AppRepoPath(appName string) string {
	return filepath.Join(c.AppDir(appName), "repo.git")
}

func (c Config) AppWorktreePath(appName string) string {
	return filepath.Join(c.AppDir(appName), "worktree")
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}

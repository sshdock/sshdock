package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	DataDir         string
	SQLiteDBPath    string
	AppsDir         string
	NodeID          string
	SSHListenAddr   string
	DashboardUser   string
	CaddyConfigPath string
}

func Default() Config {
	dataDir := "/var/lib/rhumbase"

	return Config{
		DataDir:         dataDir,
		SQLiteDBPath:    filepath.Join(dataDir, "rhumbase.db"),
		AppsDir:         filepath.Join(dataDir, "apps"),
		NodeID:          "local",
		SSHListenAddr:   ":2222",
		DashboardUser:   "dashboard",
		CaddyConfigPath: "/etc/caddy/rhumbase.caddyfile",
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
	cfg.CaddyConfigPath = envOrDefault("RHUMBASE_CADDY_CONFIG_PATH", cfg.CaddyConfigPath)

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

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	DashboardCommand            string
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
		DashboardAuthorizedKeysPath: filepath.Join(dataDir, "dashboard", ".ssh", "authorized_keys"),
		DashboardCommand:            "sudo -n -u rhumbase /usr/local/bin/rhumbase-dashboard",
		GitUser:                     "git",
		GitHomeDir:                  filepath.Join(dataDir, "git"),
		GitHost:                     "server",
		GitAuthorizedKeysPath:       filepath.Join(dataDir, "git", ".ssh", "authorized_keys"),
		GitReceiveCommand:           "sudo -n -u rhumbase /usr/local/bin/rhumbase-git-receive",
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
	cfg.DashboardAuthorizedKeysPath = envOrDefault("RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH", filepath.Join(cfg.DataDir, "dashboard", ".ssh", "authorized_keys"))
	cfg.DashboardCommand = envOrDefault("RHUMBASE_DASHBOARD_COMMAND", cfg.DashboardCommand)
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

func (c Config) Validate() error {
	required := []struct {
		env   string
		value string
	}{
		{env: "RHUMBASE_DATA_DIR", value: c.DataDir},
		{env: "RHUMBASE_SQLITE_DB_PATH", value: c.SQLiteDBPath},
		{env: "RHUMBASE_APPS_DIR", value: c.AppsDir},
		{env: "RHUMBASE_NODE_ID", value: c.NodeID},
		{env: "RHUMBASE_SSH_LISTEN_ADDR", value: c.SSHListenAddr},
		{env: "RHUMBASE_DASHBOARD_USER", value: c.DashboardUser},
		{env: "RHUMBASE_DASHBOARD_HOST_KEY_PATH", value: c.DashboardHostKeyPath},
		{env: "RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH", value: c.DashboardAuthorizedKeysPath},
		{env: "RHUMBASE_DASHBOARD_COMMAND", value: c.DashboardCommand},
		{env: "RHUMBASE_GIT_USER", value: c.GitUser},
		{env: "RHUMBASE_GIT_HOME_DIR", value: c.GitHomeDir},
		{env: "RHUMBASE_GIT_HOST", value: c.GitHost},
		{env: "RHUMBASE_GIT_AUTHORIZED_KEYS_PATH", value: c.GitAuthorizedKeysPath},
		{env: "RHUMBASE_GIT_RECEIVE_COMMAND", value: c.GitReceiveCommand},
		{env: "RHUMBASE_CADDY_CONFIG_PATH", value: c.CaddyConfigPath},
	}

	var problems []string
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			problems = append(problems, field.env+" is required")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid Rhumbase config: %s", strings.Join(problems, "; "))
	}

	return nil
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}

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
	dataDir := "/var/lib/sshdock"

	return Config{
		DataDir:                     dataDir,
		SQLiteDBPath:                filepath.Join(dataDir, "sshdock.db"),
		AppsDir:                     filepath.Join(dataDir, "apps"),
		NodeID:                      "local",
		SSHListenAddr:               ":2222",
		DashboardUser:               "dashboard",
		DashboardHostKeyPath:        filepath.Join(dataDir, "dashboard", "ssh_host_rsa_key"),
		DashboardAuthorizedKeysPath: filepath.Join(dataDir, "dashboard", ".ssh", "authorized_keys"),
		DashboardCommand:            "sudo -n -u sshdock /usr/local/bin/sshdock-dashboard",
		GitUser:                     "git",
		GitHomeDir:                  filepath.Join(dataDir, "git"),
		GitHost:                     "server",
		GitAuthorizedKeysPath:       filepath.Join(dataDir, "git", ".ssh", "authorized_keys"),
		GitReceiveCommand:           "sudo -n -u sshdock /usr/local/bin/sshdock-git-receive",
		CaddyConfigPath:             "/etc/caddy/sshdock.caddyfile",
	}
}

func LoadFromEnv() Config {
	cfg := Default()

	cfg.DataDir = envOrDefault("SSHDOCK_DATA_DIR", cfg.DataDir)
	cfg.SQLiteDBPath = envOrDefault("SSHDOCK_SQLITE_DB_PATH", filepath.Join(cfg.DataDir, "sshdock.db"))
	cfg.AppsDir = envOrDefault("SSHDOCK_APPS_DIR", filepath.Join(cfg.DataDir, "apps"))
	cfg.NodeID = envOrDefault("SSHDOCK_NODE_ID", cfg.NodeID)
	cfg.SSHListenAddr = envOrDefault("SSHDOCK_SSH_LISTEN_ADDR", cfg.SSHListenAddr)
	cfg.DashboardUser = envOrDefault("SSHDOCK_DASHBOARD_USER", cfg.DashboardUser)
	cfg.DashboardHostKeyPath = envOrDefault("SSHDOCK_DASHBOARD_HOST_KEY_PATH", filepath.Join(cfg.DataDir, "dashboard", "ssh_host_rsa_key"))
	cfg.DashboardAuthorizedKeysPath = envOrDefault("SSHDOCK_DASHBOARD_AUTHORIZED_KEYS_PATH", filepath.Join(cfg.DataDir, "dashboard", ".ssh", "authorized_keys"))
	cfg.DashboardCommand = envOrDefault("SSHDOCK_DASHBOARD_COMMAND", cfg.DashboardCommand)
	cfg.GitUser = envOrDefault("SSHDOCK_GIT_USER", cfg.GitUser)
	cfg.GitHomeDir = envOrDefault("SSHDOCK_GIT_HOME_DIR", filepath.Join(cfg.DataDir, "git"))
	cfg.GitHost = envOrDefault("SSHDOCK_GIT_HOST", cfg.GitHost)
	cfg.GitAuthorizedKeysPath = envOrDefault("SSHDOCK_GIT_AUTHORIZED_KEYS_PATH", filepath.Join(cfg.GitHomeDir, ".ssh", "authorized_keys"))
	cfg.GitReceiveCommand = envOrDefault("SSHDOCK_GIT_RECEIVE_COMMAND", cfg.GitReceiveCommand)
	cfg.CaddyConfigPath = envOrDefault("SSHDOCK_CADDY_CONFIG_PATH", cfg.CaddyConfigPath)
	cfg.CaddyAdminAddress = envOrDefault("SSHDOCK_CADDY_ADMIN_ADDRESS", cfg.CaddyAdminAddress)

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
		{env: "SSHDOCK_DATA_DIR", value: c.DataDir},
		{env: "SSHDOCK_SQLITE_DB_PATH", value: c.SQLiteDBPath},
		{env: "SSHDOCK_APPS_DIR", value: c.AppsDir},
		{env: "SSHDOCK_NODE_ID", value: c.NodeID},
		{env: "SSHDOCK_SSH_LISTEN_ADDR", value: c.SSHListenAddr},
		{env: "SSHDOCK_DASHBOARD_USER", value: c.DashboardUser},
		{env: "SSHDOCK_DASHBOARD_HOST_KEY_PATH", value: c.DashboardHostKeyPath},
		{env: "SSHDOCK_DASHBOARD_AUTHORIZED_KEYS_PATH", value: c.DashboardAuthorizedKeysPath},
		{env: "SSHDOCK_DASHBOARD_COMMAND", value: c.DashboardCommand},
		{env: "SSHDOCK_GIT_USER", value: c.GitUser},
		{env: "SSHDOCK_GIT_HOME_DIR", value: c.GitHomeDir},
		{env: "SSHDOCK_GIT_HOST", value: c.GitHost},
		{env: "SSHDOCK_GIT_AUTHORIZED_KEYS_PATH", value: c.GitAuthorizedKeysPath},
		{env: "SSHDOCK_GIT_RECEIVE_COMMAND", value: c.GitReceiveCommand},
		{env: "SSHDOCK_CADDY_CONFIG_PATH", value: c.CaddyConfigPath},
	}

	var problems []string
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			problems = append(problems, field.env+" is required")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid SSHDock config: %s", strings.Join(problems, "; "))
	}

	return nil
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}

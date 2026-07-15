package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	DataDir                    string
	SQLiteDBPath               string
	AppsDir                    string
	LocksDir                   string
	ConfigKeyPath              string
	NodeID                     string
	SSHListenAddr              string
	OperatorUser               string
	OperatorHostKeyPath        string
	OperatorAuthorizedKeysPath string
	OperatorCommand            string
	GitUser                    string
	GitHomeDir                 string
	GitHost                    string
	GitAuthorizedKeysPath      string
	GitReceiveCommand          string
	CaddyConfigPath            string
	CaddyMainConfigPath        string
	CaddyAdminAddress          string
}

func Default() Config {
	dataDir := "/var/lib/sshdock"

	return Config{
		DataDir:                    dataDir,
		SQLiteDBPath:               filepath.Join(dataDir, "sshdock.db"),
		AppsDir:                    filepath.Join(dataDir, "apps"),
		LocksDir:                   filepath.Join(dataDir, "locks"),
		ConfigKeyPath:              filepath.Join(dataDir, "config.key"),
		NodeID:                     "local",
		SSHListenAddr:              ":2222",
		OperatorUser:               "sshdock",
		OperatorHostKeyPath:        filepath.Join(dataDir, "ssh_host_rsa_key"),
		OperatorAuthorizedKeysPath: filepath.Join(dataDir, ".ssh", "authorized_keys"),
		OperatorCommand:            "/usr/local/bin/sshdock-operator",
		GitUser:                    "git",
		GitHomeDir:                 filepath.Join(dataDir, "git"),
		GitHost:                    "server",
		GitAuthorizedKeysPath:      filepath.Join(dataDir, "git", ".ssh", "authorized_keys"),
		GitReceiveCommand:          "sudo -n -u sshdock /usr/local/bin/sshdock-git-receive",
		CaddyConfigPath:            "/etc/caddy/sshdock/sshdock.caddyfile",
		CaddyMainConfigPath:        "/etc/caddy/Caddyfile",
	}
}

func LoadFromEnv() Config {
	cfg := Default()

	cfg.DataDir = envOrDefault("SSHDOCK_DATA_DIR", cfg.DataDir)
	cfg.SQLiteDBPath = envOrDefault("SSHDOCK_SQLITE_DB_PATH", filepath.Join(cfg.DataDir, "sshdock.db"))
	cfg.AppsDir = envOrDefault("SSHDOCK_APPS_DIR", filepath.Join(cfg.DataDir, "apps"))
	cfg.LocksDir = envOrDefault("SSHDOCK_LOCKS_DIR", filepath.Join(cfg.DataDir, "locks"))
	cfg.ConfigKeyPath = envOrDefault("SSHDOCK_CONFIG_KEY_PATH", filepath.Join(cfg.DataDir, "config.key"))
	cfg.NodeID = envOrDefault("SSHDOCK_NODE_ID", cfg.NodeID)
	cfg.SSHListenAddr = envOrDefault("SSHDOCK_SSH_LISTEN_ADDR", cfg.SSHListenAddr)
	cfg.OperatorUser = envOrDefault("SSHDOCK_OPERATOR_USER", cfg.OperatorUser)
	cfg.OperatorHostKeyPath = envOrDefault("SSHDOCK_OPERATOR_HOST_KEY_PATH", filepath.Join(cfg.DataDir, "ssh_host_rsa_key"))
	cfg.OperatorAuthorizedKeysPath = envOrDefault("SSHDOCK_OPERATOR_AUTHORIZED_KEYS_PATH", filepath.Join(cfg.DataDir, ".ssh", "authorized_keys"))
	cfg.OperatorCommand = envOrDefault("SSHDOCK_OPERATOR_COMMAND", cfg.OperatorCommand)
	cfg.GitUser = envOrDefault("SSHDOCK_GIT_USER", cfg.GitUser)
	cfg.GitHomeDir = envOrDefault("SSHDOCK_GIT_HOME_DIR", filepath.Join(cfg.DataDir, "git"))
	cfg.GitHost = envOrDefault("SSHDOCK_GIT_HOST", cfg.GitHost)
	cfg.GitAuthorizedKeysPath = envOrDefault("SSHDOCK_GIT_AUTHORIZED_KEYS_PATH", filepath.Join(cfg.GitHomeDir, ".ssh", "authorized_keys"))
	cfg.GitReceiveCommand = envOrDefault("SSHDOCK_GIT_RECEIVE_COMMAND", cfg.GitReceiveCommand)
	cfg.CaddyConfigPath = envOrDefault("SSHDOCK_CADDY_CONFIG_PATH", cfg.CaddyConfigPath)
	cfg.CaddyMainConfigPath = envOrDefault("SSHDOCK_CADDY_MAIN_CONFIG_PATH", cfg.CaddyMainConfigPath)
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
		{env: "SSHDOCK_LOCKS_DIR", value: c.LocksDir},
		{env: "SSHDOCK_CONFIG_KEY_PATH", value: c.ConfigKeyPath},
		{env: "SSHDOCK_NODE_ID", value: c.NodeID},
		{env: "SSHDOCK_SSH_LISTEN_ADDR", value: c.SSHListenAddr},
		{env: "SSHDOCK_OPERATOR_USER", value: c.OperatorUser},
		{env: "SSHDOCK_OPERATOR_HOST_KEY_PATH", value: c.OperatorHostKeyPath},
		{env: "SSHDOCK_OPERATOR_AUTHORIZED_KEYS_PATH", value: c.OperatorAuthorizedKeysPath},
		{env: "SSHDOCK_OPERATOR_COMMAND", value: c.OperatorCommand},
		{env: "SSHDOCK_GIT_USER", value: c.GitUser},
		{env: "SSHDOCK_GIT_HOME_DIR", value: c.GitHomeDir},
		{env: "SSHDOCK_GIT_HOST", value: c.GitHost},
		{env: "SSHDOCK_GIT_AUTHORIZED_KEYS_PATH", value: c.GitAuthorizedKeysPath},
		{env: "SSHDOCK_GIT_RECEIVE_COMMAND", value: c.GitReceiveCommand},
		{env: "SSHDOCK_CADDY_CONFIG_PATH", value: c.CaddyConfigPath},
		{env: "SSHDOCK_CADDY_MAIN_CONFIG_PATH", value: c.CaddyMainConfigPath},
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

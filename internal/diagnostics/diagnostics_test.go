package diagnostics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/store"
)

func TestRunReportsOKWhenConfigDependenciesAndDirsAreUsable(t *testing.T) {
	root := t.TempDir()
	cfg := diagnosticsConfig(root)
	prepareHealthyDiagnosticsRuntime(t, cfg)
	executor := healthyExecutor(cfg)

	report := Run(context.Background(), cfg, executor)

	if !report.OK {
		t.Fatalf("report OK = false, checks = %#v", report.Checks)
	}
	for _, want := range []string{
		"config",
		"data dir",
		"apps dir",
		"config key dir",
		"sqlite dir",
		"git home dir",
		"operating system",
		"docker",
		"docker compose",
		"caddy",
		"ssh",
		"sshd",
		"git",
		"systemd",
		"sshdockd service",
		"port 22",
		"port 80",
		"port 443",
		"base-domain DNS",
		"wildcard DNS",
		"caddy import",
		"caddy config",
		"git authorized_keys",
		"dashboard authorized_keys",
		"runtime permissions",
		"config key",
		"sqlite migrations",
	} {
		if !hasCheck(report, want, true) {
			t.Fatalf("missing ok check %q in %#v", want, report.Checks)
		}
	}
}

func TestRunReportsActionableFailures(t *testing.T) {
	cfg := diagnosticsConfig(t.TempDir())
	cfg.DataDir = filepath.Join(t.TempDir(), "missing-data")
	cfg.GitHost = " "
	executor := &fakeExecutor{Errs: map[string]error{"docker version": errors.New("docker missing")}}

	report := Run(context.Background(), cfg, executor)

	if report.OK {
		t.Fatalf("report OK = true, want false")
	}
	for _, want := range []string{
		"SSHDOCK_GIT_HOST is required",
		"data dir",
		"missing-data",
		"docker missing",
		"why docker:",
		"fix docker:",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("report missing %q:\n%s", want, report.String())
		}
	}
}

func TestRunReportsActionableInstallConfidenceFailures(t *testing.T) {
	root := t.TempDir()
	cfg := diagnosticsConfig(root)
	prepareHealthyDiagnosticsRuntime(t, cfg)

	if err := os.WriteFile(cfg.CaddyMainConfigPath, []byte("# no import\n"), 0o644); err != nil {
		t.Fatalf("WriteFile caddy main: %v", err)
	}
	if err := os.Chmod(cfg.ConfigKeyPath, 0o644); err != nil {
		t.Fatalf("Chmod config key: %v", err)
	}
	executor := healthyExecutor(cfg)
	executor.Outputs["ss -ltn"] = "LISTEN 0 4096 *:22 *:*\nLISTEN 0 4096 *:80 *:*\n"

	report := Run(context.Background(), cfg, executor)

	if report.OK {
		t.Fatalf("report OK = true, want false")
	}
	output := report.String()
	for _, want := range []string{
		"fail port 443:",
		"fix port 443: open TCP port 443",
		"fail caddy import:",
		"fix caddy import: add this exact line to " + cfg.CaddyMainConfigPath + ": import " + cfg.CaddyConfigPath,
		"fail config key:",
		"fix config key: run sudo chmod 0600 " + cfg.ConfigKeyPath,
		"diagnostics failed",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("report missing %q:\n%s", want, output)
		}
	}
}

type fakeExecutor struct {
	Commands []Command
	Errs     map[string]error
	Outputs  map[string]string
}

func (f *fakeExecutor) Run(_ context.Context, command Command) (string, error) {
	f.Commands = append(f.Commands, command)
	key := command.Name + " " + strings.Join(command.Args, " ")
	if err := f.Errs[key]; err != nil {
		return "", err
	}
	if output, ok := f.Outputs[key]; ok {
		return output, nil
	}
	return "ok", nil
}

func diagnosticsConfig(root string) config.Config {
	return config.Config{
		DataDir:                     filepath.Join(root, "data"),
		SQLiteDBPath:                filepath.Join(root, "data", "sshdock.db"),
		AppsDir:                     filepath.Join(root, "data", "apps"),
		LocksDir:                    filepath.Join(root, "data", "locks"),
		ConfigKeyPath:               filepath.Join(root, "data", "config.key"),
		NodeID:                      "local",
		SSHListenAddr:               ":2222",
		DashboardUser:               "dashboard",
		DashboardHostKeyPath:        filepath.Join(root, "data", "dashboard", "ssh_host_rsa_key"),
		DashboardAuthorizedKeysPath: filepath.Join(root, "data", "dashboard", ".ssh", "authorized_keys"),
		DashboardCommand:            "/usr/local/bin/sshdockd dashboard",
		GitUser:                     "git",
		GitHomeDir:                  filepath.Join(root, "data", "git"),
		GitHost:                     "server",
		GitAuthorizedKeysPath:       filepath.Join(root, "data", "git", ".ssh", "authorized_keys"),
		GitReceiveCommand:           "/usr/local/bin/sshdockd git-receive",
		CaddyConfigPath:             filepath.Join(root, "caddy", "sshdock.caddyfile"),
		CaddyMainConfigPath:         filepath.Join(root, "caddy", "Caddyfile"),
	}
}

func prepareHealthyDiagnosticsRuntime(t *testing.T, cfg config.Config) {
	t.Helper()
	mkdirs(t,
		cfg.DataDir,
		cfg.AppsDir,
		filepath.Dir(cfg.ConfigKeyPath),
		filepath.Dir(cfg.SQLiteDBPath),
		cfg.GitHomeDir,
		filepath.Dir(cfg.GitAuthorizedKeysPath),
		filepath.Dir(cfg.DashboardHostKeyPath),
		filepath.Dir(cfg.DashboardAuthorizedKeysPath),
		filepath.Dir(cfg.CaddyConfigPath),
		filepath.Dir(cfg.CaddyMainConfigPath),
	)
	if err := os.WriteFile(cfg.ConfigKeyPath, []byte("12345678901234567890123456789012"), 0o600); err != nil {
		t.Fatalf("WriteFile config key: %v", err)
	}
	if err := os.WriteFile(cfg.GitAuthorizedKeysPath, []byte("command=\"exec /usr/local/bin/sshdockd git-receive\",no-pty ssh-ed25519 AAAA git\n"), 0o600); err != nil {
		t.Fatalf("WriteFile git authorized_keys: %v", err)
	}
	if err := os.WriteFile(cfg.DashboardAuthorizedKeysPath, []byte("command=\"exec /usr/local/bin/sshdockd dashboard\",no-port-forwarding ssh-ed25519 AAAA dashboard\n"), 0o600); err != nil {
		t.Fatalf("WriteFile dashboard authorized_keys: %v", err)
	}
	if err := os.WriteFile(cfg.CaddyConfigPath, []byte("# SSHDock generated routes\n"), 0o644); err != nil {
		t.Fatalf("WriteFile generated caddy: %v", err)
	}
	if err := os.WriteFile(cfg.CaddyMainConfigPath, []byte("import "+cfg.CaddyConfigPath+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile caddy main: %v", err)
	}

	sqlite, err := store.OpenSQLite(context.Background(), cfg.SQLiteDBPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer sqlite.Close()
	if err := sqlite.SetServerConfig(context.Background(), store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "sshdock.example.com",
		UpdatedAt:  time.Unix(1700000000, 0).UTC(),
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
}

func healthyExecutor(cfg config.Config) *fakeExecutor {
	return &fakeExecutor{Outputs: map[string]string{
		"uname -s":                             "Linux\n",
		"docker version":                       "docker ok\n",
		"docker compose version":               "compose ok\n",
		"caddy version":                        "caddy ok\n",
		"ssh -V":                               "OpenSSH ok\n",
		"sshd -V":                              "OpenSSH ok\n",
		"git --version":                        "git version ok\n",
		"systemctl --version":                  "systemd ok\n",
		"systemctl is-active sshdockd.service": "active\n",
		"ss -ltn":                              "LISTEN 0 4096 *:22 *:*\nLISTEN 0 4096 *:80 *:*\nLISTEN 0 4096 *:443 *:*\n",
		"getent ahosts sshdock.example.com":    "203.0.113.10 STREAM sshdock.example.com\n",
		"getent ahosts sshdock-diagnostics.example.com":      "203.0.113.10 STREAM sshdock-diagnostics.example.com\n",
		"caddy validate --config " + cfg.CaddyMainConfigPath: "Valid configuration\n",
	}}
}

func hasCheck(report Report, name string, ok bool) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.OK == ok {
			return true
		}
	}
	return false
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", path, err)
		}
	}
}

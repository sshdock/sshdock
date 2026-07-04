package diagnostics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/config"
)

func TestRunReportsOKWhenConfigDependenciesAndDirsAreUsable(t *testing.T) {
	root := t.TempDir()
	cfg := diagnosticsConfig(root)
	mkdirs(t,
		cfg.DataDir,
		cfg.AppsDir,
		filepath.Dir(cfg.SQLiteDBPath),
		cfg.GitHomeDir,
		filepath.Dir(cfg.GitAuthorizedKeysPath),
		filepath.Dir(cfg.DashboardHostKeyPath),
		filepath.Dir(cfg.DashboardAuthorizedKeysPath),
		filepath.Dir(cfg.CaddyConfigPath),
	)
	executor := &fakeExecutor{}

	report := Run(context.Background(), cfg, executor)

	if !report.OK {
		t.Fatalf("report OK = false, checks = %#v", report.Checks)
	}
	for _, want := range []string{
		"config",
		"data dir",
		"apps dir",
		"sqlite dir",
		"git home dir",
		"docker",
		"docker compose",
		"caddy",
		"ssh",
		"sshd",
		"git",
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
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("report missing %q:\n%s", want, report.String())
		}
	}
}

type fakeExecutor struct {
	Commands []Command
	Errs     map[string]error
}

func (f *fakeExecutor) Run(_ context.Context, command Command) (string, error) {
	f.Commands = append(f.Commands, command)
	key := command.Name + " " + strings.Join(command.Args, " ")
	if err := f.Errs[key]; err != nil {
		return "", err
	}
	return "ok", nil
}

func diagnosticsConfig(root string) config.Config {
	return config.Config{
		DataDir:                     filepath.Join(root, "data"),
		SQLiteDBPath:                filepath.Join(root, "data", "sshdock.db"),
		AppsDir:                     filepath.Join(root, "data", "apps"),
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
	}
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

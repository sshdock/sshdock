//go:build e2e

package e2e

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/store"
)

func TestCLILifecycleEndToEnd(t *testing.T) {
	requireGit(t)

	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}

	sshdockPath := filepath.Join(binDir, "sshdock")
	sshdockdPath := filepath.Join(binDir, "sshdockd")
	runCommand(t, root, nil, "go", "build", "-o", sshdockPath, "./cmd/sshdock")
	runCommand(t, root, nil, "go", "build", "-o", sshdockdPath, "./cmd/sshdockd")
	writeFakeCaddy(t, filepath.Join(binDir, "caddy"))

	appName := "cli-lifecycle-app"
	dataDir := filepath.Join(tmp, "data")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	caddyConfigPath := filepath.Join(tmp, "Caddyfile")
	authorizedKeysPath := filepath.Join(tmp, "git", ".ssh", "authorized_keys")
	operatorAuthorizedKeysPath := filepath.Join(tmp, ".ssh", "authorized_keys")
	env := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_COMPOSE_RUNNER=fake",
		"SSHDOCK_FAKE_COMPOSE_SERVICES=web:running",
		"SSHDOCK_FAKE_COMPOSE_LOGS=web log\n",
		"SSHDOCK_CADDY_CONFIG_PATH="+caddyConfigPath,
		"SSHDOCK_GIT_AUTHORIZED_KEYS_PATH="+authorizedKeysPath,
		"SSHDOCK_OPERATOR_AUTHORIZED_KEYS_PATH="+operatorAuthorizedKeysPath,
		"SSHDOCK_GIT_RECEIVE_COMMAND="+sshdockdPath+" git-receive",
		"SSHDOCK_OPERATOR_COMMAND="+sshdockdPath+" operator",
	)
	cfg := config.LoadFromEnv()

	runCommand(t, root, env, sshdockPath, "apps", "create", appName)

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source: %v", err)
	}
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte("services:\n  web:\n    image: example/web:latest\n    restart: unless-stopped\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "initial lifecycle app")
	commitSHA := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	releaseID := app.ReleaseID(appName, commitSHA)
	runGit(t, sourceDir, nil, "remote", "add", "prod", cfg.AppRepoPath(appName))
	runGit(t, sourceDir, env, "push", "prod", "main")

	domain := "example.com"
	runCommand(t, root, env, sshdockPath, "domains", "attach", appName, "web", domain, "--port", "3000")
	publicKey := "ssh-ed25519 QUJDRA== admin@example.com\n"
	runCommandInput(t, root, env, publicKey, sshdockPath, "ssh-keys", "add", "admin")

	assertCLIOutputContains(t, root, env, sshdockPath, []string{"apps", "health", appName}, []string{
		"health: ok",
		"latest release: " + releaseID + " succeeded",
		"latest deploy:",
		"domains: 1",
		"services: 1 running, 0 attention",
	})
	assertCLIOutputContains(t, root, env, sshdockPath, []string{"logs", appName, "web", "--tail", "25"}, []string{"web log"})
	assertCLIOutputContains(t, root, env, sshdockPath, []string{"releases", "list", appName}, []string{releaseID, shortSHA(commitSHA)})
	assertCLIOutputContains(t, root, env, sshdockPath, []string{"events", "list", appName}, []string{"deploy.started", "deploy.succeeded", "domain.attached", "router.reloaded"})
	assertCLIOutputContains(t, root, env, sshdockPath, []string{"domains", "list", appName}, []string{domain, "web", "3000"})
	assertCLIOutputContains(t, root, env, sshdockPath, []string{"domains", "check", appName}, []string{domain, "web", "3000", "unavailable", "check Caddy"})
	assertCLIOutputContains(t, root, env, sshdockPath, []string{"ssh-keys", "list"}, []string{"admin", "SHA256:"})

	runCommand(t, root, env, sshdockPath, "apps", "stop", appName)
	failingStartEnv := append(append([]string{}, env...), "SSHDOCK_FAKE_COMPOSE_START_ERROR=no containers to start")
	startCmd := exec.Command(sshdockPath, "apps", "start", appName)
	startCmd.Dir = root
	startCmd.Env = failingStartEnv
	failedStartOutput, startErr := startCmd.CombinedOutput()
	if startErr == nil {
		t.Fatalf("apps start with missing containers succeeded:\n%s", failedStartOutput)
	}
	if !strings.Contains(string(failedStartOutput), "sudo sshdock apps redeploy "+appName) {
		t.Fatalf("apps start failure omitted redeploy guidance:\n%s", failedStartOutput)
	}
	runCommand(t, root, env, sshdockPath, "apps", "start", appName)
	runCommand(t, root, env, sshdockPath, "apps", "restart", appName)
	runCommand(t, root, env, sshdockPath, "apps", "redeploy", appName)
	assertCLIOutputContains(t, root, env, sshdockPath, []string{"events", "list", appName}, []string{
		"stop.started",
		"stop.succeeded",
		"start.failed",
		"start.succeeded",
		"restart.started",
		"restart.succeeded",
		"redeploy.started",
		"redeploy.succeeded",
	})

	runCommand(t, root, env, sshdockPath, "domains", "detach", appName, domain)
	domainsOutput := runCommand(t, root, env, sshdockPath, "domains", "list", appName)
	if !strings.Contains(domainsOutput, "no domains") {
		t.Fatalf("domains list after detach = %q", domainsOutput)
	}

	runCommand(t, root, env, sshdockPath, "ssh-keys", "remove", "admin")
	keysOutput := runCommand(t, root, env, sshdockPath, "ssh-keys", "list")
	if !strings.Contains(keysOutput, "no SSH keys") {
		t.Fatalf("ssh-keys list after remove = %q", keysOutput)
	}

	removeOutput := runCommand(t, root, env, sshdockPath, "apps", "remove", appName, "--force")
	if !strings.Contains(removeOutput, "Docker volumes were not removed") {
		t.Fatalf("apps remove output missing volume preservation copy:\n%s", removeOutput)
	}
	appsOutput := runCommand(t, root, env, sshdockPath, "apps", "list")
	if !strings.Contains(appsOutput, "no apps") {
		t.Fatalf("apps list after remove = %q", appsOutput)
	}
	if _, err := os.Stat(cfg.AppRepoPath(appName)); !os.IsNotExist(err) {
		t.Fatalf("repo stat after remove = %v, want not exist", err)
	}
	if _, err := os.Stat(cfg.AppWorktreePath(appName)); !os.IsNotExist(err) {
		t.Fatalf("worktree stat after remove = %v, want not exist", err)
	}
	assertAppRemovedFromSQLite(t, cfg.SQLiteDBPath, appName)
}

func writeFakeCaddy(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile fake caddy: %v", err)
	}
}

func assertCLIOutputContains(t *testing.T, dir string, env []string, sshdockPath string, args []string, wants []string) {
	t.Helper()

	output := runCommand(t, dir, env, sshdockPath, args...)
	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Fatalf("sshdock %s output missing %q:\n%s", strings.Join(args, " "), want, output)
		}
	}
}

func assertAppRemovedFromSQLite(t *testing.T, dbPath string, appID string) {
	t.Helper()

	sqlite, err := store.OpenSQLite(t.Context(), dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer sqlite.Close()

	if _, err := sqlite.GetApp(t.Context(), appID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetApp after remove error = %v, want ErrNotFound", err)
	}
	if releases, err := sqlite.ListReleasesByApp(t.Context(), appID); err != nil || len(releases) != 0 {
		t.Fatalf("releases after remove = %#v, err = %v", releases, err)
	}
	if deployments, err := sqlite.ListDeploymentsByApp(t.Context(), appID); err != nil || len(deployments) != 0 {
		t.Fatalf("deployments after remove = %#v, err = %v", deployments, err)
	}
	if domains, err := sqlite.ListDomainsByApp(t.Context(), appID); err != nil || len(domains) != 0 {
		t.Fatalf("domains after remove = %#v, err = %v", domains, err)
	}
	if events, err := sqlite.ListEventsByApp(t.Context(), appID); err != nil || !hasLifecycleEvent(events, "remove.started") || !hasLifecycleEvent(events, "remove.succeeded") {
		t.Fatalf("audit events after remove = %#v, err = %v", events, err)
	}
}

func hasLifecycleEvent(events []app.Event, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

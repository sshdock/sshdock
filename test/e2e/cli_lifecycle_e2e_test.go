//go:build e2e

package e2e

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iketiunn/rumbase/internal/config"
	"github.com/iketiunn/rumbase/internal/store"
)

func TestCLILifecycleEndToEnd(t *testing.T) {
	requireGit(t)

	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}

	rhumbasePath := filepath.Join(binDir, "rhumbase")
	rhumbasedPath := filepath.Join(binDir, "rhumbased")
	runCommand(t, root, nil, "go", "build", "-o", rhumbasePath, "./cmd/rhumbase")
	runCommand(t, root, nil, "go", "build", "-o", rhumbasedPath, "./cmd/rhumbased")
	writeFakeCaddy(t, filepath.Join(binDir, "caddy"))

	appName := "cli-lifecycle-app"
	dataDir := filepath.Join(tmp, "data")
	t.Setenv("RHUMBASE_DATA_DIR", dataDir)
	t.Setenv("RHUMBASE_COMPOSE_RUNNER", "fake")
	caddyConfigPath := filepath.Join(tmp, "Caddyfile")
	authorizedKeysPath := filepath.Join(tmp, "git", ".ssh", "authorized_keys")
	dashboardAuthorizedKeysPath := filepath.Join(tmp, "dashboard", ".ssh", "authorized_keys")
	env := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_DATA_DIR="+dataDir,
		"RHUMBASE_COMPOSE_RUNNER=fake",
		"RHUMBASE_FAKE_COMPOSE_LOGS=web log\n",
		"RHUMBASE_CADDY_CONFIG_PATH="+caddyConfigPath,
		"RHUMBASE_GIT_AUTHORIZED_KEYS_PATH="+authorizedKeysPath,
		"RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH="+dashboardAuthorizedKeysPath,
		"RHUMBASE_GIT_RECEIVE_COMMAND="+rhumbasedPath+" git-receive",
		"RHUMBASE_DASHBOARD_COMMAND="+rhumbasedPath+" dashboard",
	)
	cfg := config.LoadFromEnv()

	runCommand(t, root, env, rhumbasePath, "apps", "create", appName)

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source: %v", err)
	}
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "Rhumbase Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte("services:\n  web:\n    image: example/web:latest\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "initial lifecycle app")
	commitSHA := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	releaseID := "rel_" + shortSHA(commitSHA)
	runGit(t, sourceDir, nil, "remote", "add", "prod", cfg.AppRepoPath(appName))
	runGit(t, sourceDir, env, "push", "prod", "main")

	domain := "example.com"
	runCommand(t, root, env, rhumbasePath, "domains", "attach", appName, "web", domain, "--port", "3000")
	publicKey := "ssh-ed25519 QUJDRA== admin@example.com\n"
	runCommandInput(t, root, env, publicKey, rhumbasePath, "ssh-keys", "add", "admin")

	assertCLIOutputContains(t, root, env, rhumbasePath, []string{"logs", appName, "web"}, []string{"web log"})
	assertCLIOutputContains(t, root, env, rhumbasePath, []string{"releases", "list", appName}, []string{releaseID, shortSHA(commitSHA)})
	assertCLIOutputContains(t, root, env, rhumbasePath, []string{"events", "list", appName}, []string{"deploy.started", "deploy.succeeded", "domain.attached", "router.reloaded"})
	assertCLIOutputContains(t, root, env, rhumbasePath, []string{"domains", "list", appName}, []string{domain, "web", "3000"})
	assertCLIOutputContains(t, root, env, rhumbasePath, []string{"ssh-keys", "list"}, []string{"admin", "SHA256:"})

	runCommand(t, root, env, rhumbasePath, "domains", "detach", appName, domain)
	domainsOutput := runCommand(t, root, env, rhumbasePath, "domains", "list", appName)
	if !strings.Contains(domainsOutput, "no domains") {
		t.Fatalf("domains list after detach = %q", domainsOutput)
	}

	runCommand(t, root, env, rhumbasePath, "ssh-keys", "remove", "admin")
	keysOutput := runCommand(t, root, env, rhumbasePath, "ssh-keys", "list")
	if !strings.Contains(keysOutput, "no SSH keys") {
		t.Fatalf("ssh-keys list after remove = %q", keysOutput)
	}

	runCommand(t, root, env, rhumbasePath, "apps", "remove", appName, "--force")
	appsOutput := runCommand(t, root, env, rhumbasePath, "apps", "list")
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

func assertCLIOutputContains(t *testing.T, dir string, env []string, rhumbasePath string, args []string, wants []string) {
	t.Helper()

	output := runCommand(t, dir, env, rhumbasePath, args...)
	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Fatalf("rhumbase %s output missing %q:\n%s", strings.Join(args, " "), want, output)
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
	if events, err := sqlite.ListEventsByApp(t.Context(), appID); err != nil || len(events) != 0 {
		t.Fatalf("events after remove = %#v, err = %v", events, err)
	}
}

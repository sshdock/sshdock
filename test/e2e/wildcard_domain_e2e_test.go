//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWildcardDomainEndToEnd(t *testing.T) {
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

	dataDir := filepath.Join(tmp, "data")
	caddyConfigPath := filepath.Join(tmp, "Caddyfile")
	env := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_DATA_DIR="+dataDir,
		"RHUMBASE_COMPOSE_RUNNER=fake",
		"RHUMBASE_FAKE_COMPOSE_SERVICES=web:running,api:running,admin:running",
		"RHUMBASE_FAKE_COMPOSE_LOGS=wildcard log\n",
		"RHUMBASE_CADDY_CONFIG_PATH="+caddyConfigPath,
	)

	domainOutput := runCommand(t, root, env, rhumbasePath, "server", "domain", "set", "example.com")
	for _, want := range []string{
		"server base domain set to example.com",
		"control host: rhumbase.example.com",
		"app host pattern: <app>.example.com",
	} {
		if !strings.Contains(domainOutput, want) {
			t.Fatalf("server domain output missing %q:\n%s", want, domainOutput)
		}
	}

	appName := "wildcard-app"
	createOutput := runCommand(t, root, env, rhumbasePath, "apps", "create", appName)
	for _, want := range []string{
		"git remote add rhumbase git@rhumbase.example.com:wildcard-app.git",
		"default URL after first deploy: https://wildcard-app.example.com",
	} {
		if !strings.Contains(createOutput, want) {
			t.Fatalf("apps create output missing %q:\n%s", want, createOutput)
		}
	}
	pushLocalComposeApp(t, tmp, env, filepath.Join(dataDir, "apps", appName, "repo.git"), "initial wildcard app", fmt.Sprintf(`
services:
  web:
    image: example/web:latest
    ports:
      - "127.0.0.1:%d:80"
`, 3100))

	assertCLIOutputContains(t, root, env, rhumbasePath, []string{"domains", "list", appName}, []string{"wildcard-app.example.com", "web", "3100"})
	assertCLIOutputContains(t, root, env, rhumbasePath, []string{"events", "list", appName}, []string{"deploy.succeeded", "route.auto_attached", "router.reloaded"})

	caddyConfig := readFile(t, caddyConfigPath)
	for _, want := range []string{
		"wildcard-app.example.com {",
		"reverse_proxy 127.0.0.1:3100",
	} {
		if !strings.Contains(caddyConfig, want) {
			t.Fatalf("Caddyfile missing %q:\n%s", want, caddyConfig)
		}
	}
	if strings.Contains(caddyConfig, "*.example.com") {
		t.Fatalf("Caddyfile should not contain a wildcard route:\n%s", caddyConfig)
	}

	dashboardOutput := runCommand(t, root, env, rhumbasedPath, "dashboard")
	if !strings.Contains(dashboardOutput, "wildcard-app.example.com") {
		t.Fatalf("dashboard output missing auto route:\n%s", dashboardOutput)
	}

	ambiguousApp := "ambiguous-app"
	runCommand(t, root, env, rhumbasePath, "apps", "create", ambiguousApp)
	pushLocalComposeApp(t, tmp, env, filepath.Join(dataDir, "apps", ambiguousApp, "repo.git"), "ambiguous wildcard app", `
services:
  api:
    image: example/api:latest
    ports:
      - "4100:80"
  admin:
    image: example/admin:latest
    ports:
      - "4200:80"
`)

	domainsOutput := runCommand(t, root, env, rhumbasePath, "domains", "list", ambiguousApp)
	if !strings.Contains(domainsOutput, "no domains") {
		t.Fatalf("ambiguous domains output = %q, want no domains", domainsOutput)
	}
	assertCLIOutputContains(t, root, env, rhumbasePath, []string{"events", "list", ambiguousApp}, []string{"deploy.succeeded", "route.auto_skipped", "ambiguous", "domains attach"})

	caddyConfig = readFile(t, caddyConfigPath)
	if strings.Contains(caddyConfig, "ambiguous-app.example.com") {
		t.Fatalf("ambiguous app should not have a Caddy route:\n%s", caddyConfig)
	}
	if !strings.Contains(caddyConfig, "wildcard-app.example.com") {
		t.Fatalf("existing auto route should remain after ambiguous skip:\n%s", caddyConfig)
	}
}

func pushLocalComposeApp(t *testing.T, tmp string, env []string, repoPath string, message string, composeContent string) string {
	t.Helper()

	sourceDir := filepath.Join(tmp, strings.ReplaceAll(message, " ", "-"))
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source: %v", err)
	}
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "Rhumbase Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte(strings.TrimSpace(composeContent)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", message)
	commitSHA := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	runGit(t, sourceDir, nil, "remote", "add", "prod", repoPath)
	runGit(t, sourceDir, env, "push", "prod", "main")
	return commitSHA
}

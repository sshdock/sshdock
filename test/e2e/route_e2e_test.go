//go:build e2e

package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestRouteThroughCaddyEndToEnd(t *testing.T) {
	requireDocker(t)
	caddyPath := requireCommandOrSkip(t, "caddy")
	curlPath := requireCommandOrSkip(t, "curl")

	paths := setupBootstrappedServerPush(t, "docker")
	appName := "route-app"
	projectName := compose.ProjectName(appName)
	servicePort := freeLocalPort(t)
	caddyPort := freeLocalPort(t)
	caddyAdminPort := freeLocalPort(t)
	caddyAdminAddress := fmt.Sprintf("127.0.0.1:%d", caddyAdminPort)
	caddyConfigPath := filepath.Join(paths.tmp, "Caddyfile")

	if err := os.WriteFile(caddyConfigPath, []byte(fmt.Sprintf("{\n\tadmin %s\n}\n", caddyAdminAddress)), 0o644); err != nil {
		t.Fatalf("WriteFile initial Caddyfile: %v", err)
	}
	caddyCtx, cancelCaddy := context.WithCancel(context.Background())
	caddyCmd := exec.CommandContext(caddyCtx, caddyPath, "run", "--config", caddyConfigPath)
	caddyLogPath := filepath.Join(paths.tmp, "caddy.log")
	caddyLog, err := os.Create(caddyLogPath)
	if err != nil {
		cancelCaddy()
		t.Fatalf("Create caddy log: %v", err)
	}
	caddyCmd.Stdout = caddyLog
	caddyCmd.Stderr = caddyLog
	if err := caddyCmd.Start(); err != nil {
		cancelCaddy()
		_ = caddyLog.Close()
		t.Skipf("start Caddy: %v", err)
	}
	t.Cleanup(func() {
		cancelCaddy()
		_ = caddyCmd.Wait()
		_ = caddyLog.Close()
	})
	waitForTCP(t, "127.0.0.1", caddyAdminPort, caddyLogPath)

	commitSHA := pushComposeAppThroughSSH(t, paths, appName, map[string]string{
		"compose.yml": fmt.Sprintf("services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"127.0.0.1:%d:80\"\n", servicePort),
	})
	worktreePath := filepath.Join(paths.dataDir, "apps", appName, "worktree")
	composePath := filepath.Join(worktreePath, "compose.yml")
	t.Cleanup(func() {
		_ = runCommandNoFail(worktreePath, nil, "docker", "compose", "-f", composePath, "-p", projectName, "down", "-v", "--remove-orphans")
	})

	routeDomain := fmt.Sprintf("http://127.0.0.1:%d", caddyPort)
	cliEnv := append(os.Environ(),
		"PATH="+paths.installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+paths.dataDir,
		"SSHDOCK_CADDY_CONFIG_PATH="+caddyConfigPath,
		"SSHDOCK_CADDY_ADMIN_ADDRESS="+caddyAdminAddress,
	)
	runCommand(t, filepath.Join("..", ".."), cliEnv, filepath.Join(paths.installBinDir, "sshdock"), "domains", "attach", appName, "web", routeDomain, "--port", fmt.Sprintf("%d", servicePort))

	config := readFile(t, caddyConfigPath)
	for _, want := range []string{
		"admin " + caddyAdminAddress,
		routeDomain + " {",
		fmt.Sprintf("reverse_proxy 127.0.0.1:%d", servicePort),
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("Caddyfile missing %q:\n%s", want, config)
		}
	}

	waitForCurl(t, curlPath, fmt.Sprintf("http://127.0.0.1:%d", caddyPort), "Welcome to nginx", caddyLogPath)
	assertEventTypes(t, filepath.Join(paths.dataDir, "sshdock.db"), appName, []string{"deploy.started", "deploy.succeeded", "domain.attached", "router.reloaded"})
	assertDomainRow(t, filepath.Join(paths.dataDir, "sshdock.db"), appName, routeDomain, servicePort)
	_ = commitSHA
}

func waitForCurl(t *testing.T, curlPath string, url string, want string, logPath string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var lastOutput []byte
	var lastErr error
	for time.Now().Before(deadline) {
		cmd := exec.Command(curlPath, "-fsS", url)
		lastOutput, lastErr = cmd.CombinedOutput()
		if lastErr == nil && strings.Contains(string(lastOutput), want) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s through Caddy: %v\ncurl output:\n%s\ncaddy log:\n%s", url, lastErr, lastOutput, readFile(t, logPath))
}

func assertDomainRow(t *testing.T, dbPath string, appID string, domainName string, port int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	var gotPort int
	err = db.QueryRow(`select port from domains where app_id = ? and domain_name = ?`, appID, domainName).Scan(&gotPort)
	if err != nil {
		t.Fatalf("query domain: %v", err)
	}
	if gotPort != port {
		t.Fatalf("domain port = %d, want %d", gotPort, port)
	}
}

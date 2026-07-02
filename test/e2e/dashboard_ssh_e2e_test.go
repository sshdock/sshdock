//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardSSHSessionEndToEnd(t *testing.T) {
	requireGit(t)
	sshPath := requireCommandOrSkip(t, "ssh")
	sshKeygenPath := requireCommandOrSkip(t, "ssh-keygen")
	paths := setupBootstrappedServerPush(t, "fake")

	appName := "dashboard-app"
	pushComposeAppThroughSSH(t, paths, appName, map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	})

	dashboardKeyPath := filepath.Join(paths.tmp, "dashboard_ed25519")
	runCommand(t, paths.tmp, nil, sshKeygenPath, "-t", "ed25519", "-N", "", "-f", dashboardKeyPath)
	dashboardAuthorizedKeysPath := filepath.Join(paths.tmp, "dashboard_authorized_keys")
	if err := os.WriteFile(dashboardAuthorizedKeysPath, []byte(readFile(t, dashboardKeyPath+".pub")), 0o600); err != nil {
		t.Fatalf("WriteFile dashboard authorized keys: %v", err)
	}

	port := freeLocalPort(t)
	dashboardHostKeyPath := filepath.Join(paths.tmp, "dashboard_host_rsa_key")
	dashboardLogPath := filepath.Join(paths.tmp, "dashboard.log")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rhumbasedPath := filepath.Join(paths.installBinDir, "rhumbased")
	env := append(os.Environ(),
		"PATH="+paths.installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_DATA_DIR="+paths.dataDir,
		"RHUMBASE_SSH_LISTEN_ADDR=127.0.0.1:"+fmt.Sprintf("%d", port),
		"RHUMBASE_DASHBOARD_USER=dashboard",
		"RHUMBASE_DASHBOARD_HOST_KEY_PATH="+dashboardHostKeyPath,
		"RHUMBASE_DASHBOARD_AUTHORIZED_KEYS_PATH="+dashboardAuthorizedKeysPath,
		"RHUMBASE_COMPOSE_RUNNER=fake",
		"RHUMBASE_FAKE_COMPOSE_SERVICES=web:running",
		"RHUMBASE_FAKE_COMPOSE_LOGS=first dashboard log\nsecond dashboard log\n",
	)
	dashboard := exec.CommandContext(ctx, rhumbasedPath, "serve")
	dashboard.Env = env
	logFile, err := os.Create(dashboardLogPath)
	if err != nil {
		t.Fatalf("Create dashboard log: %v", err)
	}
	dashboard.Stdout = logFile
	dashboard.Stderr = logFile
	if err := dashboard.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start dashboard server: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = dashboard.Wait()
		_ = logFile.Close()
	})
	waitForTCP(t, "127.0.0.1", port, dashboardLogPath)

	output := runCommand(t, paths.tmp, nil,
		sshPath,
		"-p", fmt.Sprintf("%d", port),
		"-i", dashboardKeyPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"dashboard@127.0.0.1",
		"dashboard",
	)
	for _, want := range []string{
		"Rhumbase Dashboard",
		appName,
		"healthy",
		"latest=succeeded",
		"Services",
		"web running",
		"Releases",
		"Deployments",
		"Logs web",
		"first dashboard log",
		"second dashboard log",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("dashboard output missing %q:\n%s\ndashboard log:\n%s", want, output, readFile(t, dashboardLogPath))
		}
	}
}

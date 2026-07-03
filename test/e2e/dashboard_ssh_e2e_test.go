//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDashboardSSHSessionEndToEnd(t *testing.T) {
	requireGit(t)
	sshdPath := requireCommandOrSkip(t, "sshd")
	sshPath := requireCommandOrSkip(t, "ssh")
	sshKeygenPath := requireCommandOrSkip(t, "ssh-keygen")
	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("current user: %v", err)
	}
	if currentUser.Username == "" {
		t.Skip("current user name is required for OpenSSH dashboard e2e")
	}
	paths := setupBootstrappedServerPush(t, "fake")

	appName := "dashboard-app"
	pushComposeAppThroughSSH(t, paths, appName, map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	})

	hostKeyPath := filepath.Join(paths.tmp, "dashboard_host_ed25519")
	runCommand(t, paths.tmp, nil, sshKeygenPath, "-t", "ed25519", "-N", "", "-f", hostKeyPath)
	port := freeLocalPort(t)
	sshdConfigPath := filepath.Join(paths.tmp, "dashboard_sshd_config")
	sshdLogPath := filepath.Join(paths.tmp, "dashboard_sshd.log")
	sshdConfig := fmt.Sprintf(`
Port %d
ListenAddress 127.0.0.1
HostKey %s
PidFile %s
AuthorizedKeysFile %s
PasswordAuthentication no
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
PubkeyAuthentication yes
StrictModes no
AllowUsers %s
LogLevel ERROR
`, port, hostKeyPath, filepath.Join(paths.tmp, "dashboard_sshd.pid"), paths.dashboardAuthorizedKeysPath, currentUser.Username)
	if err := os.WriteFile(sshdConfigPath, []byte(sshdConfig), 0o600); err != nil {
		t.Fatalf("WriteFile dashboard sshd_config: %v", err)
	}
	if output, err := exec.Command(sshdPath, "-t", "-f", sshdConfigPath).CombinedOutput(); err != nil {
		t.Skipf("OpenSSH dashboard config is not usable in this environment: %v\n%s", err, output)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sshd := exec.CommandContext(ctx, sshdPath, "-D", "-e", "-f", sshdConfigPath)
	logFile, err := os.Create(sshdLogPath)
	if err != nil {
		t.Fatalf("Create dashboard sshd log: %v", err)
	}
	sshd.Stdout = logFile
	sshd.Stderr = logFile
	if err := sshd.Start(); err != nil {
		_ = logFile.Close()
		t.Skipf("start dashboard sshd: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = sshd.Wait()
		_ = logFile.Close()
	})
	waitForTCP(t, "127.0.0.1", port, sshdLogPath)

	plainOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		"-T",
		"-p", fmt.Sprintf("%d", port),
		"-i", paths.clientKeyPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		currentUser.Username+"@127.0.0.1",
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
		"first-dashboard-log",
	} {
		if !strings.Contains(plainOutput, want) {
			t.Fatalf("plain dashboard output missing %q:\n%s\ndashboard sshd log:\n%s", want, plainOutput, readFile(t, sshdLogPath))
		}
	}

	interactiveEnv := append(os.Environ(), "TERM=xterm-256color")
	terminalQueryResponsesTabAndQuit := "\x1b]11;rgb:0000/0000/0000\x07\x1b[1;1R\tq"
	interactiveOutput := runCommandDelayedInput(t, paths.tmp, interactiveEnv, terminalQueryResponsesTabAndQuit, 750*time.Millisecond, 15*time.Second,
		sshPath,
		"-tt",
		"-p", fmt.Sprintf("%d", port),
		"-i", paths.clientKeyPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		currentUser.Username+"@127.0.0.1",
	)
	for _, want := range []string{
		"Rhumbase",
		appName,
		"Service",
		"State",
		"web",
		"running",
	} {
		if !strings.Contains(interactiveOutput, want) {
			t.Fatalf("interactive dashboard output missing %q:\n%s\ndashboard sshd log:\n%s", want, interactiveOutput, readFile(t, sshdLogPath))
		}
	}
	if strings.Contains(interactiveOutput, "PTY allocation request failed") {
		t.Fatalf("interactive dashboard failed PTY allocation:\n%s", interactiveOutput)
	}
}

func runCommandDelayedInput(t *testing.T, dir string, env []string, input string, inputDelay time.Duration, timeout time.Duration, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("%s stdin pipe: %v", name, err)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("%s %s start failed: %v", name, strings.Join(args, " "), err)
	}
	go func() {
		time.Sleep(inputDelay)
		_, _ = io.WriteString(stdin, input)
		_ = stdin.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output.String())
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		t.Fatalf("%s %s timed out after %s\n%s", name, strings.Join(args, " "), timeout, output.String())
	}
	return output.String()
}

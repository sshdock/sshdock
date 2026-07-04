//go:build e2e

package e2e

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDashboardSSHSessionEndToEnd(t *testing.T) {
	requireGit(t)
	sshdPath := requireCommandOrSkip(t, "sshd")
	sshPath := requireCommandOrSkip(t, "ssh")
	sshKeygenPath := requireCommandOrSkip(t, "ssh-keygen")
	paths := setupBootstrappedServerPush(t, "fake")

	appName := "dashboard-app"
	pushComposeAppThroughSSH(t, paths, appName, map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	})

	server := startDashboardSSHServer(t, paths, sshdPath, sshKeygenPath)

	plainOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		dashboardSSHArgs(paths, server, false)...,
	)
	for _, want := range []string{
		"SSHDock Dashboard",
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
			t.Fatalf("plain dashboard output missing %q:\n%s\ndashboard sshd log:\n%s", want, plainOutput, readFile(t, server.LogPath))
		}
	}

	interactiveEnv := append(os.Environ(), "TERM=xterm-256color")
	terminalQueryResponsesTabAndQuit := "\x1b]11;rgb:0000/0000/0000\x07\x1b[1;1R\tq"
	interactiveOutput := runCommandDelayedInput(t, paths.tmp, interactiveEnv, terminalQueryResponsesTabAndQuit, 750*time.Millisecond, 15*time.Second,
		sshPath,
		dashboardSSHArgs(paths, server, true)...,
	)
	for _, want := range []string{
		"SSHDock",
		appName,
		"Service",
		"State",
		"web",
		"running",
	} {
		if !strings.Contains(interactiveOutput, want) {
			t.Fatalf("interactive dashboard output missing %q:\n%s\ndashboard sshd log:\n%s", want, interactiveOutput, readFile(t, server.LogPath))
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

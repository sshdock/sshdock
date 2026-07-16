//go:build e2e

package e2e

import (
	"bytes"
	"io"
	"os"
	"os/exec"
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
		"Current main:",
		"Latest deploy: dep_",
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

func TestDashboardSSHRestrictedOperatorCommandsEndToEnd(t *testing.T) {
	// Given
	requireGit(t)
	sshdPath := requireCommandOrSkip(t, "sshd")
	sshPath := requireCommandOrSkip(t, "ssh")
	sshKeygenPath := requireCommandOrSkip(t, "ssh-keygen")
	paths := setupBootstrappedServerPush(t, "fake")
	appName := "operator-app"
	pushComposeAppThroughSSH(t, paths, appName, map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	})
	operatorEnv := append(os.Environ(),
		"PATH="+filepath.Join(paths.tmp, "fake-bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+paths.dataDir,
		"SSHDOCK_COMPOSE_RUNNER=fake",
		"SSHDOCK_CADDY_CONFIG_PATH="+filepath.Join(paths.tmp, "operator.caddyfile"),
	)
	runCommand(t, paths.tmp, operatorEnv,
		filepath.Join(paths.installBinDir, "sshdock"),
		"domains", "attach", appName, "web", "operator.example.com", "--port", "3000",
	)
	server := startDashboardSSHServer(t, paths, sshdPath, sshKeygenPath)

	// When
	inspectionOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "apps", "list")...,
	)
	configOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "config", "list", appName)...,
	)
	healthOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "apps", "health", appName)...,
	)
	logsOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "logs", appName)...,
	)
	domainsOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "domains", "check", appName)...,
	)
	helpOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "help")...,
	)
	stopOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "apps", "stop", appName)...,
	)
	startOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "apps", "start", appName)...,
	)
	restartOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "apps", "restart", appName)...,
	)
	redeployOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "apps", "redeploy", appName)...,
	)
	execOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), `apps exec `+appName+` web -- printf "%s" "value with spaces"`)...,
	)
	runOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), `apps run `+appName+` web -- sh -c "printf one-off"`)...,
	)
	ptyExecOutput := runCommand(t, paths.tmp, append(os.Environ(), "TERM=xterm-256color"),
		sshPath,
		append(dashboardSSHArgs(paths, server, true), `apps exec `+appName+` web -- printf interactive`)...,
	)
	eventsOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "events", "list", appName)...,
	)

	// Then
	if !strings.Contains(inspectionOutput, appName) {
		t.Fatalf("apps list output missing %q:\n%s", appName, inspectionOutput)
	}
	if !strings.Contains(configOutput, "no config") {
		t.Fatalf("config list output missing empty state:\n%s", configOutput)
	}
	for _, want := range []string{"current main:", "latest deploy: dep_", "commit=", "trigger=push", "routes: 0 active, 1 attention (unavailable=1)", "services: 1 running, 0 attention"} {
		if !strings.Contains(healthOutput, want) {
			t.Fatalf("apps health output missing %q:\n%s", want, healthOutput)
		}
	}
	if !strings.Contains(logsOutput, "first-dashboard-log") {
		t.Fatalf("logs output missing Compose logs:\n%s", logsOutput)
	}
	if !strings.Contains(domainsOutput, "unavailable") || !strings.Contains(domainsOutput, "check Caddy") {
		t.Fatalf("domains check output did not report unavailable active Caddy state:\n%s", domainsOutput)
	}
	if strings.Contains(helpOutput, "server domain") || !strings.Contains(helpOutput, "apps health") || !strings.Contains(helpOutput, "apps start") || !strings.Contains(helpOutput, "apps exec") || !strings.Contains(helpOutput, "apps run") || !strings.Contains(helpOutput, "apps remove") {
		t.Fatalf("restricted help exposes local commands or omits inspection commands:\n%s", helpOutput)
	}
	if !strings.Contains(execOutput, "exec-output") || !strings.Contains(runOutput, "run-output") || !strings.Contains(ptyExecOutput, "exec-output") {
		t.Fatalf("restricted service command output missing: exec=%q run=%q pty-exec=%q", execOutput, runOutput, ptyExecOutput)
	}
	for label, output := range map[string]string{
		"stop":     stopOutput,
		"start":    startOutput,
		"restart":  restartOutput,
		"redeploy": redeployOutput,
	} {
		if !strings.Contains(output, appName) {
			t.Fatalf("apps %s output missing app name:\n%s", label, output)
		}
	}
	for _, eventType := range []string{"stop.started", "stop.succeeded", "start.started", "start.succeeded", "restart.started", "restart.succeeded", "redeploy.started", "redeploy.succeeded", "exec.started", "exec.succeeded", "run.started", "run.succeeded"} {
		if !strings.Contains(eventsOutput, eventType) {
			t.Fatalf("events output missing %q:\n%s", eventType, eventsOutput)
		}
	}

	markerPath := filepath.Join(paths.tmp, "host-shell-ran")
	maliciousCommand := "apps list; touch " + markerPath
	args := append(dashboardSSHArgs(paths, server, false), maliciousCommand)
	cmd := exec.Command(sshPath, args...)
	cmd.Dir = paths.tmp
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("host-shell command succeeded:\n%s", output)
	}
	if !strings.Contains(string(output), "not available over SSH") {
		t.Fatalf("invalid-command output missing restricted guidance:\n%s", output)
	}
	if _, statErr := os.Stat(markerPath); !os.IsNotExist(statErr) {
		t.Fatalf("host shell marker exists: %v", statErr)
	}

	removeOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "apps", "remove", appName, "--force")...,
	)
	if !strings.Contains(removeOutput, "Docker volumes were not removed") {
		t.Fatalf("restricted app removal omitted volume-preservation copy:\n%s", removeOutput)
	}
	removedEventsOutput := runCommand(t, paths.tmp, nil,
		sshPath,
		append(dashboardSSHArgs(paths, server, false), "events", "list", appName)...,
	)
	if !strings.Contains(removedEventsOutput, "remove.started") || !strings.Contains(removedEventsOutput, "remove.succeeded") {
		t.Fatalf("removed app audit history missing lifecycle events:\n%s", removedEventsOutput)
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

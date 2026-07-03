package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iketiunn/rumbase/internal/compose"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(version) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	want := "rhumbased dev\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestHookRunnerFromEnvSelectsDockerRunner(t *testing.T) {
	t.Setenv("RHUMBASE_COMPOSE_RUNNER", "docker")

	runner, err := hookRunnerFromEnv()
	if err != nil {
		t.Fatalf("hookRunnerFromEnv: %v", err)
	}
	if _, ok := runner.(*compose.DockerRunner); !ok {
		t.Fatalf("runner = %T, want *compose.DockerRunner", runner)
	}
}

func TestHookRunnerFromEnvConfiguresFakeDeployError(t *testing.T) {
	t.Setenv("RHUMBASE_COMPOSE_RUNNER", "fake")
	t.Setenv("RHUMBASE_FAKE_COMPOSE_DEPLOY_ERROR", "compose failed")

	runner, err := hookRunnerFromEnv()
	if err != nil {
		t.Fatalf("hookRunnerFromEnv: %v", err)
	}
	fake, ok := runner.(*compose.FakeRunner)
	if !ok {
		t.Fatalf("runner = %T, want *compose.FakeRunner", runner)
	}
	if fake.DeployErr == nil || fake.DeployErr.Error() != "compose failed" {
		t.Fatalf("DeployErr = %v, want compose failed", fake.DeployErr)
	}
}

func TestDashboardRunnerFromEnvConfiguresFakeStatusAndLogs(t *testing.T) {
	t.Setenv("RHUMBASE_COMPOSE_RUNNER", "fake")
	t.Setenv("RHUMBASE_FAKE_COMPOSE_SERVICES", "web:running,worker:exited")
	t.Setenv("RHUMBASE_FAKE_COMPOSE_LOGS", "first log\nsecond log\n")

	runner, err := dashboardRunnerFromEnv()
	if err != nil {
		t.Fatalf("dashboardRunnerFromEnv: %v", err)
	}
	fake, ok := runner.(*compose.FakeRunner)
	if !ok {
		t.Fatalf("runner = %T, want *compose.FakeRunner", runner)
	}
	if len(fake.Services) != 2 || fake.Services[0].Name != "web" || fake.Services[0].State != "running" || fake.Services[1].Name != "worker" || fake.Services[1].State != "exited" {
		t.Fatalf("fake services = %#v", fake.Services)
	}
	if fake.LogOutput != "first log\nsecond log\n" {
		t.Fatalf("fake log output = %q", fake.LogOutput)
	}
}

func TestRunDashboardRendersOnce(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("RHUMBASE_DATA_DIR", dataDir)
	t.Setenv("RHUMBASE_SQLITE_DB_PATH", filepath.Join(dataDir, "rhumbase.db"))
	t.Setenv("RHUMBASE_COMPOSE_RUNNER", "fake")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithInput([]string{"dashboard"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"Rhumbase Dashboard",
		"Apps",
		"No apps",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestDashboardHasInteractiveTerminalRejectsNonTTYWriters(t *testing.T) {
	if dashboardHasInteractiveTerminal(strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("dashboardHasInteractiveTerminal returned true for non-TTY streams")
	}
}

func TestRunDaemonValidatesConfig(t *testing.T) {
	t.Setenv("RHUMBASE_GIT_HOST", " ")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithInput([]string{"daemon"}, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "RHUMBASE_GIT_HOST is required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunGitReceiveRequiresSSHOriginalCommand(t *testing.T) {
	t.Setenv("SSH_ORIGINAL_COMMAND", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithInput([]string{"git-receive"}, nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "SSH_ORIGINAL_COMMAND") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunGitReceiveValidatesConfig(t *testing.T) {
	t.Setenv("SSH_ORIGINAL_COMMAND", "git-receive-pack 'my-app.git'")
	t.Setenv("RHUMBASE_GIT_HOST", " ")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithInput([]string{"git-receive"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "RHUMBASE_GIT_HOST is required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

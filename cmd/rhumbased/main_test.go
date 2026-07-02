package main

import (
	"bytes"
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

package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := NewRunner(NewMemoryBackend("server"), "dev")

	code := runner.Run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "rhumbase dev\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestAppsCreatePrintsRemoteNextSteps(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := NewRunner(NewMemoryBackend("example.com"), "dev")

	code := runner.Run([]string{"apps", "create", "my-app"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"created app my-app",
		"git remote add prod git@example.com:my-app",
		"git push prod main",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout missing %q:\n%s", want, output)
		}
	}
}

func TestAppsListAndInfo(t *testing.T) {
	backend := NewMemoryBackend("server")
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runner.Run([]string{"apps", "create", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("create exit code = %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"apps", "list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("list exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "my-app\tcreated\tlocal") {
		t.Fatalf("list stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"apps", "info", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("info exit code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"name: my-app", "status: created", "node: local"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("info stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestDomainsAttach(t *testing.T) {
	backend := NewMemoryBackend("server")
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runner.Run([]string{"apps", "create", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("create exit code = %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	code := runner.Run([]string{"domains", "attach", "my-app", "web", "example.com", "--port", "3000"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("domains attach exit code = %d, stderr = %q", code, stderr.String())
	}

	for _, want := range []string{
		"attached example.com to my-app/web:3000",
		"point DNS for example.com to this server",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestUnknownCommandPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := NewRunner(NewMemoryBackend("server"), "dev")

	code := runner.Run([]string{"unknown"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("exit code = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "usage: rhumbase") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

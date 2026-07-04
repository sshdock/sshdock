package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestVersionCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := NewRunner(NewMemoryBackend("server"), "dev")

	code := runner.Run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "sshdock dev\n" {
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
		"git remote add sshdock git@example.com:my-app.git",
		"git push sshdock main",
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

func TestSSHKeysAddReadsPublicKeyFromInput(t *testing.T) {
	backend := NewMemoryBackend("server")
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runner.RunWithInput(
		[]string{"ssh-keys", "add", "admin"},
		strings.NewReader("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com\n"),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("ssh-keys add exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "added SSH key admin") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestLifecycleInspectionCommands(t *testing.T) {
	backend := NewMemoryBackend("server")
	backend.apps["my-app"] = App{Name: "my-app", Status: "healthy", NodeID: "local"}
	backend.releases = []Release{{
		ID:        "rel_1",
		AppName:   "my-app",
		CommitSHA: "abc123",
		Status:    "succeeded",
		CreatedAt: time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC),
	}}
	backend.events = []Event{{
		AppName:   "my-app",
		Type:      "deploy.succeeded",
		Message:   "Deploy succeeded",
		CreatedAt: time.Date(2026, 7, 4, 10, 1, 0, 0, time.UTC),
	}}
	backend.domains = []Domain{{
		AppName:     "my-app",
		ServiceName: "web",
		DomainName:  "example.com",
		Port:        3000,
		HTTPS:       true,
	}}
	backend.keys["admin"] = SSHKey{
		Name:      "admin",
		PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com",
		CreatedAt: time.Date(2026, 7, 4, 10, 2, 0, 0, time.UTC),
	}
	backend.logOutput = "web log\n"
	runner := NewRunner(backend, "dev")

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "logs", args: []string{"logs", "my-app", "web", "-f"}, want: []string{"web log\n"}},
		{name: "releases list", args: []string{"releases", "list", "my-app"}, want: []string{"rel_1\tsucceeded\tabc123\t2026-07-04T10:00:00Z"}},
		{name: "events list", args: []string{"events", "list", "my-app"}, want: []string{"2026-07-04T10:01:00Z\tdeploy.succeeded\tDeploy succeeded"}},
		{name: "domains list", args: []string{"domains", "list", "my-app"}, want: []string{"example.com\tweb\t3000\ttrue"}},
		{name: "ssh-keys list", args: []string{"ssh-keys", "list"}, want: []string{"admin\t", "\t2026-07-04T10:02:00Z"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if code := runner.Run(test.args, &stdout, &stderr); code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			for _, want := range test.want {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
				}
			}
		})
	}
	if len(backend.logRequests) != 1 {
		t.Fatalf("logRequests = %#v", backend.logRequests)
	}
	if request := backend.logRequests[0]; request.AppName != "my-app" || request.ServiceName != "web" || !request.Follow {
		t.Fatalf("log request = %#v", request)
	}
}

func TestLifecycleMutationCommands(t *testing.T) {
	backend := NewMemoryBackend("server")
	backend.apps["my-app"] = App{Name: "my-app", Status: "healthy", NodeID: "local"}
	backend.domains = []Domain{{AppName: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true}}
	backend.keys["admin"] = SSHKey{Name: "admin", PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com"}
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runner.Run([]string{"domains", "detach", "my-app", "example.com"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains detach exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "detached example.com from my-app") {
		t.Fatalf("domains detach stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"ssh-keys", "remove", "admin"}, &stdout, &stderr); code != 0 {
		t.Fatalf("ssh-keys remove exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "removed SSH key admin") {
		t.Fatalf("ssh-keys remove stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.RunWithInput([]string{"apps", "remove", "my-app"}, strings.NewReader("wrong\n"), &stdout, &stderr); code != 1 {
		t.Fatalf("apps remove wrong confirmation exit code = %d, want 1", code)
	}
	if _, ok := backend.apps["my-app"]; !ok {
		t.Fatal("app was removed after wrong confirmation")
	}

	stdout.Reset()
	stderr.Reset()
	if code := runner.RunWithInput([]string{"apps", "remove", "my-app"}, strings.NewReader("my-app\n"), &stdout, &stderr); code != 0 {
		t.Fatalf("apps remove exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "removed app my-app") {
		t.Fatalf("apps remove stdout = %q", stdout.String())
	}
	if _, ok := backend.apps["my-app"]; ok {
		t.Fatal("app still exists after remove")
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
	if !strings.Contains(stderr.String(), "usage: sshdock") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

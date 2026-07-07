package cli

import (
	"bytes"
	"fmt"
	"os"
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

func TestRootHelpPrintsGroupedCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no args", args: nil},
		{name: "help", args: []string{"help"}},
		{name: "long flag", args: []string{"--help"}},
		{name: "short flag", args: []string{"-h"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			runner := NewRunner(NewMemoryBackend("server"), "dev")

			code := runner.Run(test.args, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			output := stdout.String()
			for _, want := range []string{
				"SSHDock - Git push Compose apps. Operate over SSH.",
				"Usage:",
				"  sshdock <command> [arguments]",
				"Core:",
				"Apps:",
				"Config:",
				"Domains:",
				"Operations:",
				"Access:",
				"Server:",
				"  config keys <app>",
				`Use "sshdock help <command>" for details.`,
			} {
				if !strings.Contains(output, want) {
					t.Fatalf("stdout missing %q:\n%s", want, output)
				}
			}
			if strings.Contains(output, "usage: sshdock version |") {
				t.Fatalf("stdout still has single-line usage:\n%s", output)
			}
		})
	}
}

func TestGroupHelpPrintsUsageAndExamples(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := NewRunner(NewMemoryBackend("server"), "dev")

	code := runner.Run([]string{"help", "config"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"Config commands store encrypted app config.",
		"Usage:",
		"  sshdock config set <app> <key> [--scope <scope>]",
		"  sshdock config keys <app>",
		"Examples:",
		`  printf '%s' "$DATABASE_URL" | ssh dashboard@<host> config set my-app DATABASE_URL`,
		"  ssh dashboard@<host> config keys my-app",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout missing %q:\n%s", want, output)
		}
	}
}

func TestCommandHelpFlagPrintsGroupHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := NewRunner(NewMemoryBackend("server"), "dev")

	code := runner.Run([]string{"config", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Config commands store encrypted app config.") {
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

func TestConfigCommandsRedactListAndRevealOnlyOnGet(t *testing.T) {
	backend := NewMemoryBackend("server")
	backend.apps["my-app"] = App{Name: "my-app", Status: "created", NodeID: "local"}
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runner.RunWithInput([]string{"config", "set", "my-app", "DATABASE_URL"}, strings.NewReader("postgres://secret\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config set exit code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "postgres://secret") || strings.Contains(stderr.String(), "postgres://secret") {
		t.Fatalf("config set leaked secret stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "redeploy required for running containers") || !strings.Contains(stdout.String(), "sudo sshdock apps redeploy my-app") {
		t.Fatalf("config set stdout missing redeploy hint:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runner.Run([]string{"config", "list", "my-app"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config list exit code = %d, stderr = %q", code, stderr.String())
	}
	listOutput := stdout.String()
	if !strings.Contains(listOutput, "DATABASE_URL\t-\tset\t<redacted>") {
		t.Fatalf("config list stdout = %q", listOutput)
	}
	if strings.Contains(listOutput, "postgres://secret") {
		t.Fatalf("config list leaked secret: %q", listOutput)
	}

	stdout.Reset()
	stderr.Reset()
	code = runner.Run([]string{"config", "keys", "my-app"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config keys exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "DATABASE_URL\n" {
		t.Fatalf("config keys stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runner.Run([]string{"config", "get", "my-app", "DATABASE_URL"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config get exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "postgres://secret\n" {
		t.Fatalf("config get stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runner.Run([]string{"config", "unset", "my-app", "DATABASE_URL"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config unset exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "unset config DATABASE_URL for my-app") {
		t.Fatalf("config unset stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "redeploy required for running containers") || !strings.Contains(stdout.String(), "sudo sshdock apps redeploy my-app") {
		t.Fatalf("config unset stdout missing redeploy hint:\n%s", stdout.String())
	}
}

func TestConfigImportSupportsScope(t *testing.T) {
	backend := NewMemoryBackend("server")
	backend.apps["my-app"] = App{Name: "my-app", Status: "created", NodeID: "local"}
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runner.RunWithInput(
		[]string{"config", "import", "my-app", "--scope", "worker"},
		strings.NewReader("API_TOKEN=worker-secret\n"),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("config import exit code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "worker-secret") || strings.Contains(stderr.String(), "worker-secret") {
		t.Fatalf("config import leaked secret stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "redeploy required for running containers") || !strings.Contains(stdout.String(), "sudo sshdock apps redeploy my-app") {
		t.Fatalf("config import stdout missing redeploy hint:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runner.Run([]string{"config", "list", "my-app"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config list exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "API_TOKEN\tworker\tset\t<redacted>") {
		t.Fatalf("config list stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runner.Run([]string{"config", "keys", "my-app"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config keys scoped exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "worker/API_TOKEN\n" {
		t.Fatalf("config keys scoped stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runner.Run([]string{"config", "get", "my-app", "API_TOKEN", "--scope", "worker"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config get scoped exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "worker-secret\n" {
		t.Fatalf("config get scoped stdout = %q", stdout.String())
	}
}

func TestConfigGetPermissionDeniedPrintsRevealGuidance(t *testing.T) {
	backend := &configGetErrorBackend{
		MemoryBackend: NewMemoryBackend("server"),
		err:           fmt.Errorf("read config encryption key /var/lib/sshdock/config.key: %w", os.ErrPermission),
	}
	backend.apps["my-app"] = App{Name: "my-app", Status: "created", NodeID: "local"}
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runner.Run([]string{"config", "get", "my-app", "DATABASE_URL", "--scope", "worker"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("config get exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("config get stdout = %q, want empty", stdout.String())
	}
	output := stderr.String()
	for _, want := range []string{
		"config get requires access to SSHDock's config encryption key",
		"sudo sshdock config get my-app DATABASE_URL --scope worker",
		"ssh dashboard@<host> config get my-app DATABASE_URL --scope worker",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stderr missing %q:\n%s", want, output)
		}
	}
	for _, avoid := range []string{"/var/lib/sshdock/config.key", "permission denied"} {
		if strings.Contains(output, avoid) {
			t.Fatalf("stderr leaked %q:\n%s", avoid, output)
		}
	}
}

type configGetErrorBackend struct {
	*MemoryBackend
	err error
}

func (b *configGetErrorBackend) GetConfig(appName string, name string, scope string) (string, error) {
	return "", b.err
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
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	output := stderr.String()
	for _, want := range []string{
		`unknown command "unknown"`,
		`Run "sshdock help" for available commands.`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stderr missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "usage: sshdock version |") {
		t.Fatalf("stderr still has single-line usage:\n%s", output)
	}
}

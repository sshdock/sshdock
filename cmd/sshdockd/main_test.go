package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/cli"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(version) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	want := "sshdockd dev\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestHookRunnerFromEnvSelectsDockerRunner(t *testing.T) {
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "docker")

	runner, err := hookRunnerFromEnv()
	if err != nil {
		t.Fatalf("hookRunnerFromEnv: %v", err)
	}
	if _, ok := runner.(*compose.DockerRunner); !ok {
		t.Fatalf("runner = %T, want *compose.DockerRunner", runner)
	}
}

func TestHookRunnerFromEnvConfiguresFakeDeployError(t *testing.T) {
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	t.Setenv("SSHDOCK_FAKE_COMPOSE_DEPLOY_ERROR", "compose failed")

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

func TestHookRunnerFromEnvConfiguresFakeDeployRouteResult(t *testing.T) {
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	t.Setenv("SSHDOCK_FAKE_COMPOSE_ROUTE", "web:3100")

	runner, err := hookRunnerFromEnv()
	if err != nil {
		t.Fatalf("hookRunnerFromEnv: %v", err)
	}
	fake, ok := runner.(*compose.FakeRunner)
	if !ok {
		t.Fatalf("runner = %T, want *compose.FakeRunner", runner)
	}
	if !fake.DeployResult.RouteFound || fake.DeployResult.RouteTarget != (compose.RouteTarget{ServiceName: "web", Port: 3100}) {
		t.Fatalf("DeployResult = %#v, want web:3100 route", fake.DeployResult)
	}
}

func TestHookRunnerFromEnvConfiguresFakeDeployRouteReason(t *testing.T) {
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	t.Setenv("SSHDOCK_FAKE_COMPOSE_ROUTE_REASON", "effective Compose model route is ambiguous")

	runner, err := hookRunnerFromEnv()
	if err != nil {
		t.Fatalf("hookRunnerFromEnv: %v", err)
	}
	fake := runner.(*compose.FakeRunner)
	if fake.DeployResult.RouteReason != "effective Compose model route is ambiguous" {
		t.Fatalf("DeployResult = %#v", fake.DeployResult)
	}
}

func TestDashboardRunnerFromEnvConfiguresFakeStatusAndLogs(t *testing.T) {
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	t.Setenv("SSHDOCK_FAKE_COMPOSE_SERVICES", "web:running,worker:exited")
	t.Setenv("SSHDOCK_FAKE_COMPOSE_LOGS", "first log\nsecond log\n")

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
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(dataDir, "sshdock.db"))
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithInput([]string{"operator"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"SSHDock Dashboard",
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

func TestRunOperatorDispatchesRestrictedAppInspectionCommand(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(dataDir, "sshdock.db"))
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	t.Setenv("SSH_ORIGINAL_COMMAND", "apps list")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	code := runWithInput([]string{"operator"}, nil, &stdout, &stderr)

	// Then
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "no apps\n" {
		t.Fatalf("stdout = %q, want no apps", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunOperatorHelpListsOnlyRemoteCommands(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(dataDir, "sshdock.db"))
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	t.Setenv("SSH_ORIGINAL_COMMAND", "help")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	code := runWithInput([]string{"operator"}, nil, &stdout, &stderr)

	// Then
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"apps list", "apps health", "apps start", "apps stop", "apps restart", "apps redeploy", "apps remove", "config set", "domains check", "logs"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	for _, unwanted := range []string{"apps create", "server domain", "ssh-keys"} {
		if strings.Contains(stdout.String(), unwanted) {
			t.Fatalf("stdout contains local-only command %q:\n%s", unwanted, stdout.String())
		}
	}
}

func TestDashboardHasInteractiveTerminalRejectsNonTTYWriters(t *testing.T) {
	if dashboardHasInteractiveTerminal(strings.NewReader(""), &bytes.Buffer{}) {
		t.Fatal("dashboardHasInteractiveTerminal returned true for non-TTY streams")
	}
}

func TestOperatorOriginalCommandArgs(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		want         []string
		errorMessage string
	}{
		{
			name:    "preserves quoted argv boundaries",
			command: `config get "my app" 'DATABASE URL'`,
			want:    []string{"config", "get", "my app", "DATABASE URL"},
		},
		{
			name:    "allows app start",
			command: `apps start my-app`,
			want:    []string{"apps", "start", "my-app"},
		},
		{
			name:    "allows app stop",
			command: `apps stop my-app`,
			want:    []string{"apps", "stop", "my-app"},
		},
		{
			name:    "allows app restart",
			command: `apps restart my-app web`,
			want:    []string{"apps", "restart", "my-app", "web"},
		},
		{
			name:    "allows current main redeploy",
			command: `apps redeploy my-app`,
			want:    []string{"apps", "redeploy", "my-app"},
		},
		{
			name:    "allows forced app removal",
			command: `apps remove my-app --force`,
			want:    []string{"apps", "remove", "my-app", "--force"},
		},
		{
			name:         "rejects interactive app removal",
			command:      `apps remove my-app`,
			errorMessage: "not available over SSH",
		},
		{
			name:         "rejects host shell syntax",
			command:      `apps list; id`,
			errorMessage: "not available over SSH",
		},
		{
			name:         "reports unterminated quotes",
			command:      `config get my-app "DATABASE_URL`,
			errorMessage: "unterminated quote",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			originalCommand := test.command

			// When
			args, err := operatorOriginalCommandArgs(originalCommand)

			// Then
			if test.errorMessage != "" {
				if err == nil || !strings.Contains(err.Error(), test.errorMessage) {
					t.Fatalf("error = %v, want message containing %q", err, test.errorMessage)
				}
				return
			}
			if err != nil {
				t.Fatalf("operatorOriginalCommandArgs: %v", err)
			}
			if strings.Join(args, "\x00") != strings.Join(test.want, "\x00") {
				t.Fatalf("args = %#v, want %#v", args, test.want)
			}
		})
	}
}

func TestDashboardActionBackendMapsToCLIBackend(t *testing.T) {
	backend := &fakeDashboardCLIBackend{}
	adapter := dashboardActionBackend{backend: backend}

	if err := adapter.RestartApp("app"); err != nil {
		t.Fatalf("RestartApp: %v", err)
	}
	if err := adapter.RestartService("app", "web"); err != nil {
		t.Fatalf("RestartService: %v", err)
	}
	if err := adapter.RedeployApp("app"); err != nil {
		t.Fatalf("RedeployApp: %v", err)
	}
	if err := adapter.RollbackApp("app", "rel_1"); err != nil {
		t.Fatalf("RollbackApp: %v", err)
	}
	if err := adapter.AttachDomain("app", "web", "example.com", 3000); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	if err := adapter.DetachDomain("app", "example.com"); err != nil {
		t.Fatalf("DetachDomain: %v", err)
	}
	if err := adapter.RemoveApp("app"); err != nil {
		t.Fatalf("RemoveApp: %v", err)
	}

	want := strings.Join([]string{
		"restart-app app",
		"restart-service app web",
		"redeploy app",
		"rollback app rel_1",
		"attach app web example.com 3000",
		"detach app example.com",
		"remove app",
	}, "\n")
	if got := strings.Join(backend.calls, "\n"); got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestRunDaemonValidatesConfig(t *testing.T) {
	t.Setenv("SSHDOCK_GIT_HOST", " ")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithInput([]string{"daemon"}, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "SSHDOCK_GIT_HOST is required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

type fakeDashboardCLIBackend struct {
	calls []string
}

func (f *fakeDashboardCLIBackend) StartApp(appName string) error {
	f.calls = append(f.calls, "start "+appName)
	return nil
}

func (f *fakeDashboardCLIBackend) StopApp(appName string) error {
	f.calls = append(f.calls, "stop "+appName)
	return nil
}

func (f *fakeDashboardCLIBackend) RestartApp(appName string) error {
	f.calls = append(f.calls, fmt.Sprintf("restart-app %s", appName))
	return nil
}

func (f *fakeDashboardCLIBackend) RestartService(appName string, serviceName string) error {
	f.calls = append(f.calls, fmt.Sprintf("restart-service %s %s", appName, serviceName))
	return nil
}

func (f *fakeDashboardCLIBackend) RedeployApp(appName string) error {
	f.calls = append(f.calls, fmt.Sprintf("redeploy %s", appName))
	return nil
}

func (f *fakeDashboardCLIBackend) RollbackApp(appName string, releaseID string) error {
	f.calls = append(f.calls, fmt.Sprintf("rollback %s %s", appName, releaseID))
	return nil
}

func (f *fakeDashboardCLIBackend) AttachDomain(domain cli.Domain) error {
	f.calls = append(f.calls, fmt.Sprintf("attach %s %s %s %d", domain.AppName, domain.ServiceName, domain.DomainName, domain.Port))
	return nil
}

func (f *fakeDashboardCLIBackend) DetachDomain(appName string, domainName string) error {
	f.calls = append(f.calls, fmt.Sprintf("detach %s %s", appName, domainName))
	return nil
}

func (f *fakeDashboardCLIBackend) RemoveApp(appName string) error {
	f.calls = append(f.calls, fmt.Sprintf("remove %s", appName))
	return nil
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
	t.Setenv("SSHDOCK_GIT_HOST", " ")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithInput([]string{"git-receive"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "SSHDOCK_GIT_HOST is required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunGitHookReportsAcceptedMainBeforeSetupFailure(t *testing.T) {
	t.Setenv("SSHDOCK_GIT_HOST", " ")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	input := strings.NewReader("oldsha newsha refs/heads/main\n")

	code := runWithInput([]string{"git-hook", "--app", "my-app", "--repo", "/apps/my-app/repo.git"}, input, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"git: remote main updated to newsha", "deploy: setup failed after remote main update", "SSHDOCK_GIT_HOST is required"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunGitPreReceiveAcceptsMainUpdate(t *testing.T) {
	// Given
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	input := strings.NewReader("oldsha newsha refs/heads/main\n")

	// When
	code := runWithInput([]string{"git-pre-receive"}, input, &stdout, &stderr)

	// Then
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
}

func TestRunGitPreReceiveRejectsNonMainUpdate(t *testing.T) {
	// Given
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	input := strings.NewReader("oldsha newsha refs/heads/feature\n")

	// When
	code := runWithInput([]string{"git-pre-receive"}, input, &stdout, &stderr)

	// Then
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "push to remote main") {
		t.Fatalf("stderr = %q, want remote-main guidance", stderr.String())
	}
}

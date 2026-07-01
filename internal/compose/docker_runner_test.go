package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDockerRunnerDeployConstructsSafeReleaseCommands(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `services:
  api:
    image: example/api:latest
  web:
    build: .
`)
	executor := &recordingExecutor{}
	runner := NewDockerRunner(executor)

	err := runner.Deploy(ctx, DeployRequest{
		AppName:               "my-app",
		ProjectDir:            projectDir,
		ComposePath:           composePath,
		CommitSHA:             "abc123",
		SuccessfulReleaseSHAs: []string{"prev-1", "prev-2", "prev-3", "prev-4", "prev-5", "old-1"},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	overridePath := filepath.Join(projectDir, ".rhumbase", "release-abc123.compose.yml")
	override, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("read override: %v", err)
	}
	if !strings.Contains(string(override), "rhumbase/my-app/web:abc123") {
		t.Fatalf("override does not contain release image tag:\n%s", override)
	}
	if strings.Contains(string(override), "latest") {
		t.Fatalf("override should deploy commit tags, not latest:\n%s", override)
	}

	want := []Command{
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "rhumbase_my-app", "config"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "rhumbase_my-app", "pull"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-f", overridePath, "-p", "rhumbase_my-app", "build", "web"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-f", overridePath, "-p", "rhumbase_my-app", "up", "-d"}},
		{Name: "docker", Dir: projectDir, Args: []string{"image", "tag", "rhumbase/my-app/web:abc123", "rhumbase/my-app/web:latest"}},
		{Name: "docker", Dir: projectDir, Args: []string{"image", "rm", "rhumbase/my-app/web:old-1"}},
	}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}

	for _, command := range executor.Commands {
		joined := strings.Join(command.Args, " ")
		if strings.Contains(joined, "system prune") {
			t.Fatalf("unexpected broad cleanup command: %#v", command)
		}
		if strings.Contains(joined, "rhumbase/my-app/api") {
			t.Fatalf("image-only service should not receive Rhumbase build tags: %#v", command)
		}
	}
}

func TestDockerRunnerUpdatesLatestOnlyAfterUpSucceeds(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `services:
  web:
    build: .
`)
	executor := &recordingExecutor{FailAt: 3, Err: errors.New("up failed")}
	runner := NewDockerRunner(executor)

	err := runner.Deploy(ctx, DeployRequest{
		AppName:     "my-app",
		ProjectDir:  projectDir,
		ComposePath: composePath,
		CommitSHA:   "abc123",
	})
	if !errors.Is(err, executor.Err) {
		t.Fatalf("Deploy error = %v, want %v", err, executor.Err)
	}

	for _, command := range executor.Commands {
		if strings.Contains(strings.Join(command.Args, " "), ":latest") {
			t.Fatalf("latest tag was updated after failed deploy: %#v", command)
		}
	}
}

func TestDockerRunnerValidateRestartStatusAndLogsCommands(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `services:
  web:
    image: example/web:latest
`)
	executor := &recordingExecutor{
		Outputs: []string{
			"",
			"",
			`[{"Service":"web","State":"running"}]`,
			"web log line\n",
		},
	}
	runner := NewDockerRunner(executor)

	result, err := runner.Validate(ctx, composePath)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(result.Services) != 1 || result.Services[0] != "web" {
		t.Fatalf("Validate services = %#v", result.Services)
	}

	restart := RestartRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath, ServiceName: "web"}
	if err := runner.Restart(ctx, restart); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	statuses, err := runner.Status(ctx, StatusRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Name != "web" || statuses[0].State != "running" {
		t.Fatalf("Status = %#v", statuses)
	}

	logs, err := runner.Logs(ctx, LogsRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath, ServiceName: "web", Lines: 50})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if logs != "web log line\n" {
		t.Fatalf("Logs = %q", logs)
	}

	want := []Command{
		{Name: "docker", Dir: filepath.Dir(composePath), Args: []string{"compose", "-f", composePath, "config"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "rhumbase_my-app", "restart", "web"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "rhumbase_my-app", "ps", "--format", "json"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "rhumbase_my-app", "logs", "--tail", "50", "web"}},
	}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}
}

type recordingExecutor struct {
	Commands []Command
	Outputs  []string
	FailAt   int
	Err      error
}

func (r *recordingExecutor) Run(_ context.Context, command Command) (string, error) {
	r.Commands = append(r.Commands, command)
	index := len(r.Commands) - 1
	if r.Err != nil && index == r.FailAt {
		return "", r.Err
	}
	if index < len(r.Outputs) {
		return r.Outputs[index], nil
	}
	return "", nil
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

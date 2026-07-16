package compose

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDockerRunnerClassifiesDeployCommandFailures(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `services:
  web:
    build: .
`)

	tests := []struct {
		name   string
		failAt int
		stage  DeployStage
	}{
		{name: "config", failAt: 0, stage: DeployStageComposeConfig},
		{name: "pull", failAt: 1, stage: DeployStagePullImages},
		{name: "build", failAt: 2, stage: DeployStageBuildServices},
		{name: "wait", failAt: 3, stage: DeployStageWaitServices},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := errors.New(test.name + " failed")
			executor := &recordingExecutor{Outputs: []string{`{"services":{"web":{"build":{"context":"."}}}}`}, FailAt: test.failAt, Err: failure}
			runner := NewDockerRunner(executor)

			_, err := runner.Deploy(ctx, DeployRequest{
				AppName:     "my-app",
				ProjectDir:  projectDir,
				ComposePath: composePath,
				CommitSHA:   "abc123",
			})
			if !errors.Is(err, failure) {
				t.Fatalf("Deploy error = %v, want wrapped %v", err, failure)
			}
			var deployErr *DeployError
			if !errors.As(err, &deployErr) {
				t.Fatalf("Deploy error = %T %[1]v, want DeployError", err)
			}
			if deployErr.Stage != test.stage {
				t.Fatalf("DeployError stage = %q, want %q", deployErr.Stage, test.stage)
			}
		})
	}
}

func TestDockerRunnerReportsParentCancellationWithoutClaimingWaitTimeout(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, "services:\n  web:\n    image: example/web:latest\n")
	ctx, cancel := context.WithCancel(context.Background())
	executor := &cancelBeforeWaitExecutor{cancel: cancel}
	runner := NewDockerRunner(executor)

	// When
	_, err := runner.Deploy(ctx, DeployRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath})

	// Then
	if err == nil || !strings.Contains(err.Error(), "deployment context") {
		t.Fatalf("Deploy error = %v, want parent-context interruption", err)
	}
	if strings.Contains(err.Error(), "exceeded 2m") {
		t.Fatalf("Deploy error falsely claimed the runner timeout: %v", err)
	}
}

func TestDockerRunnerHostDeadlineAllowsComposeWaitDiagnostics(t *testing.T) {
	if defaultDeployHostWait <= defaultDeployWait {
		t.Fatalf("host wait = %s, want longer than Compose wait %s", defaultDeployHostWait, defaultDeployWait)
	}
}

func TestDockerRunnerDeployPassesConfigAsCommandEnvironment(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `services:
  web:
    image: example/web:latest
    environment:
      DATABASE_URL: ${DATABASE_URL}
`)
	executor := &recordingExecutor{Outputs: []string{`{"services":{"web":{"image":"example/web:latest"}}}`}}
	runner := NewDockerRunner(executor)

	_, err := runner.Deploy(ctx, DeployRequest{
		AppName:     "my-app",
		ProjectDir:  projectDir,
		ComposePath: composePath,
		CommitSHA:   "abc123",
		Env:         map[string]string{"DATABASE_URL": "postgres://secret"},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(executor.Commands) != 4 {
		t.Fatalf("commands = %#v, want config/pull/build/up", executor.Commands)
	}
	for _, command := range executor.Commands {
		if command.Env["DATABASE_URL"] != "postgres://secret" {
			t.Fatalf("command missing DATABASE_URL env: %#v", command)
		}
		if strings.Contains(strings.Join(command.Args, " "), "postgres://secret") {
			t.Fatalf("secret leaked into command args: %#v", command)
		}
	}
}

func TestDockerRunnerDeployNeverRunsSSHDockImageCleanup(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `services:
  web:
    build: .
`)
	executor := &recordingExecutor{Outputs: []string{`{"services":{"web":{"build":{"context":"."}}}}`}}
	runner := NewDockerRunner(executor)

	_, err := runner.Deploy(ctx, DeployRequest{
		AppName:     "my-app",
		ProjectDir:  projectDir,
		ComposePath: composePath,
		CommitSHA:   "abc123",
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	for _, command := range executor.Commands {
		if command.Name != "docker" || len(command.Args) == 0 || command.Args[0] != "compose" {
			t.Fatalf("unexpected image-management command: %#v", command)
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

	result, err := runner.Validate(ctx, "my-app", composePath)
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

	inspectEnv := map[string]string{"DATABASE_URL": "postgres://secret"}
	statuses, err := runner.Status(ctx, StatusRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath, Env: inspectEnv})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Name != "web" || statuses[0].State != "running" {
		t.Fatalf("Status = %#v", statuses)
	}

	logs, err := runner.Logs(ctx, LogsRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath, ServiceName: "web", Lines: 50, Follow: true, Env: inspectEnv})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if logs != "web log line\n" {
		t.Fatalf("Logs = %q", logs)
	}

	want := []Command{
		{Name: "docker", Dir: filepath.Dir(composePath), Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "config"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "restart", "web"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "ps", "--all", "--format", "json"}, Env: inspectEnv},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "logs", "--follow", "--tail", "50", "web"}, Env: inspectEnv},
	}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}
}

func TestDockerRunnerRemoveUsesComposeDownWithoutVolumesOrImageCleanup(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `services:
  web:
    image: example/web:latest
`)
	executor := &recordingExecutor{}
	runner := NewDockerRunner(executor)

	if err := runner.Remove(ctx, RemoveRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	want := []Command{
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "down", "--remove-orphans"}},
	}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}
	for _, command := range executor.Commands {
		joined := strings.Join(command.Args, " ")
		if strings.Contains(joined, "--volumes") || strings.Contains(joined, " -v") {
			t.Fatalf("remove should preserve volumes, got command %#v", command)
		}
		if command.Name == "docker" && len(command.Args) > 0 && command.Args[0] == "image" {
			t.Fatalf("remove should leave image garbage collection to Docker, got command %#v", command)
		}
	}
}

func TestDockerRunnerStreamLogsUsesStreamingExecutor(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	executor := &streamingRecordingExecutor{Output: "streamed log\n"}
	runner := NewDockerRunner(executor)
	var stdout strings.Builder
	var stderr strings.Builder

	err := runner.StreamLogs(ctx, LogsRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath, ServiceName: "web", Lines: 100, Follow: true}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}
	if stdout.String() != "streamed log\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	want := []Command{{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "logs", "--follow", "--tail", "100", "web"}}}
	if !reflect.DeepEqual(executor.Streamed, want) {
		t.Fatalf("streamed commands = %#v, want %#v", executor.Streamed, want)
	}
}

func TestParseServiceStatusesAcceptsJSONArrayAndJSONLines(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{
			name:   "array",
			output: `[{"Service":"web","State":"running"}]`,
		},
		{
			name: "json lines",
			output: `{"Service":"web","State":"running"}
{"Name":"worker","State":"exited"}
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statuses, err := parseServiceStatuses(test.output)
			if err != nil {
				t.Fatalf("parseServiceStatuses: %v", err)
			}
			if len(statuses) == 0 {
				t.Fatalf("statuses = %#v", statuses)
			}
			if statuses[0].Name != "web" || statuses[0].State != "running" {
				t.Fatalf("first status = %#v", statuses[0])
			}
			if test.name == "json lines" && (len(statuses) != 2 || statuses[1].Name != "worker" || statuses[1].State != "exited") {
				t.Fatalf("json lines statuses = %#v", statuses)
			}
		})
	}
}

type recordingExecutor struct {
	Commands  []Command
	Outputs   []string
	Deadlines []bool
	FailAt    int
	Err       error
}

type streamingRecordingExecutor struct {
	Streamed []Command
	Output   string
	Err      error
}

type cancelBeforeWaitExecutor struct {
	commands int
	cancel   context.CancelFunc
}

func (e *cancelBeforeWaitExecutor) Run(ctx context.Context, _ Command) (string, error) {
	e.commands++
	if e.commands == 1 {
		return `{"services":{"web":{"image":"example/web:latest"}}}`, nil
	}
	if e.commands == 3 {
		e.cancel()
		return "", nil
	}
	if e.commands == 4 {
		return "", ctx.Err()
	}
	return "", nil
}

func (r *recordingExecutor) Run(ctx context.Context, command Command) (string, error) {
	r.Commands = append(r.Commands, command)
	_, hasDeadline := ctx.Deadline()
	r.Deadlines = append(r.Deadlines, hasDeadline)
	index := len(r.Commands) - 1
	if r.Err != nil && index == r.FailAt {
		return "", r.Err
	}
	if index < len(r.Outputs) {
		return r.Outputs[index], nil
	}
	return "", nil
}

func (r *streamingRecordingExecutor) Run(_ context.Context, command Command) (string, error) {
	return "", nil
}

func (r *streamingRecordingExecutor) Stream(_ context.Context, command Command, stdout io.Writer, _ io.Writer) error {
	r.Streamed = append(r.Streamed, command)
	if r.Err != nil {
		return r.Err
	}
	_, err := fmt.Fprint(stdout, r.Output)
	return err
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

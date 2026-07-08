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

	overridePath := filepath.Join(projectDir, ".sshdock", "release-abc123.compose.yml")
	override, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("read override: %v", err)
	}
	if !strings.Contains(string(override), "sshdock/my-app/web:abc123") {
		t.Fatalf("override does not contain release image tag:\n%s", override)
	}
	if strings.Contains(string(override), "latest") {
		t.Fatalf("override should deploy commit tags, not latest:\n%s", override)
	}

	want := []Command{
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "config"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "pull", "--ignore-buildable"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-f", overridePath, "-p", "sshdock_my-app", "build", "web"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-f", overridePath, "-p", "sshdock_my-app", "up", "-d"}},
		{Name: "docker", Dir: projectDir, Args: []string{"image", "tag", "sshdock/my-app/web:abc123", "sshdock/my-app/web:latest"}},
		{Name: "docker", Dir: projectDir, Args: []string{"image", "rm", "sshdock/my-app/web:prev-5"}},
		{Name: "docker", Dir: projectDir, Args: []string{"image", "rm", "sshdock/my-app/web:old-1"}},
	}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}

	for _, command := range executor.Commands {
		joined := strings.Join(command.Args, " ")
		if strings.Contains(joined, "system prune") {
			t.Fatalf("unexpected broad cleanup command: %#v", command)
		}
		if strings.Contains(joined, "sshdock/my-app/api") {
			t.Fatalf("image-only service should not receive SSHDock build tags: %#v", command)
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
		{name: "pull", failAt: 1, stage: DeployStagePullImages},
		{name: "build", failAt: 2, stage: DeployStageBuildServices},
		{name: "start", failAt: 3, stage: DeployStageStartContainers},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := errors.New(test.name + " failed")
			executor := &recordingExecutor{FailAt: test.failAt, Err: failure}
			runner := NewDockerRunner(executor)

			err := runner.Deploy(ctx, DeployRequest{
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
	executor := &recordingExecutor{}
	runner := NewDockerRunner(executor)

	err := runner.Deploy(ctx, DeployRequest{
		AppName:     "my-app",
		ProjectDir:  projectDir,
		ComposePath: composePath,
		CommitSHA:   "abc123",
		Env:         map[string]string{"DATABASE_URL": "postgres://secret"},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(executor.Commands) != 3 {
		t.Fatalf("commands = %#v, want config/pull/up", executor.Commands)
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

func TestDockerRunnerRecordsCleanupFailureWithoutFailingDeploy(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `services:
  web:
    build: .
`)
	executor := &recordingExecutor{FailAt: 5, Err: errors.New("image is in use")}
	recorder := &recordingCleanupRecorder{}
	runner := NewDockerRunner(executor)

	err := runner.Deploy(ctx, DeployRequest{
		AppName:               "my-app",
		ProjectDir:            projectDir,
		ComposePath:           composePath,
		CommitSHA:             "abc123",
		SuccessfulReleaseSHAs: []string{"prev-1", "prev-2", "prev-3", "prev-4", "prev-5", "old-1"},
		CleanupRecorder:       recorder,
	})
	if err != nil {
		t.Fatalf("Deploy error = %v, want nil cleanup warning", err)
	}
	if len(recorder.Failures) != 1 {
		t.Fatalf("cleanup failures = %#v", recorder.Failures)
	}
	failure := recorder.Failures[0]
	if failure.AppName != "my-app" || failure.ServiceName != "web" || failure.CommitSHA != "prev-5" || failure.Image != "sshdock/my-app/web:prev-5" || failure.ErrorMessage != "image is in use" {
		t.Fatalf("cleanup failure = %#v", failure)
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
		{Name: "docker", Dir: filepath.Dir(composePath), Args: []string{"compose", "-f", composePath, "config"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "restart", "web"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "ps", "--format", "json"}, Env: inspectEnv},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "logs", "--follow", "--tail", "50", "web"}, Env: inspectEnv},
	}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}
}

func TestDockerRunnerRemoveUsesComposeDownWithoutVolumesAndScopedImageCleanup(t *testing.T) {
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
			"sshdock/my-app/web:abc123\nsshdock/my-app/web:latest\n",
		},
	}
	runner := NewDockerRunner(executor)

	if err := runner.Remove(ctx, RemoveRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	want := []Command{
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "down", "--remove-orphans"}},
		{Name: "docker", Dir: projectDir, Args: []string{"image", "ls", "--format", "{{.Repository}}:{{.Tag}}", "--filter", "reference=sshdock/my-app/*"}},
		{Name: "docker", Dir: projectDir, Args: []string{"image", "rm", "sshdock/my-app/web:abc123"}},
		{Name: "docker", Dir: projectDir, Args: []string{"image", "rm", "sshdock/my-app/web:latest"}},
	}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}
	for _, command := range executor.Commands {
		joined := strings.Join(command.Args, " ")
		if strings.Contains(joined, "--volumes") || strings.Contains(joined, " -v") {
			t.Fatalf("remove should preserve volumes, got command %#v", command)
		}
		if strings.Contains(joined, "system prune") {
			t.Fatalf("remove should not run broad prune, got command %#v", command)
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
	Commands []Command
	Outputs  []string
	FailAt   int
	Err      error
}

type recordingCleanupRecorder struct {
	Failures []CleanupFailure
}

type streamingRecordingExecutor struct {
	Streamed []Command
	Output   string
	Err      error
}

func (r *recordingCleanupRecorder) RecordCleanupFailure(_ context.Context, failure CleanupFailure) error {
	r.Failures = append(r.Failures, failure)
	return nil
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

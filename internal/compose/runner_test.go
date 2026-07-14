package compose

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFakeRunnerValidateSuccessAndFailure(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("validation failed")
	runner := &FakeRunner{
		Validation: ValidationResult{Services: []string{"web"}},
	}

	result, err := runner.Validate(ctx, "my-app", "compose.yml")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(result.Services) != 1 || result.Services[0] != "web" {
		t.Fatalf("Validation services = %#v", result.Services)
	}
	if runner.ValidatedPath != "compose.yml" {
		t.Fatalf("ValidatedPath = %q", runner.ValidatedPath)
	}
	if runner.ValidatedAppName != "my-app" {
		t.Fatalf("ValidatedAppName = %q", runner.ValidatedAppName)
	}

	runner.ValidateErr = failure
	_, err = runner.Validate(ctx, "my-app", "compose.yml")
	if !errors.Is(err, failure) {
		t.Fatalf("Validate error = %v, want %v", err, failure)
	}
}

func TestFakeRunnerDeploySuccessAndFailure(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("deploy failed")
	runner := &FakeRunner{}
	request := DeployRequest{
		AppName:      "my-app",
		ProjectDir:   "/data/apps/my-app/worktree",
		ComposePath:  "compose.yml",
		ReleaseID:    "rel_1",
		CommitSHA:    "abc123",
		KeepReleases: 5,
	}

	if err := runner.Deploy(ctx, request); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !reflect.DeepEqual(runner.DeployRequests[0], request) {
		t.Fatalf("DeployRequests = %#v", runner.DeployRequests)
	}

	runner.DeployErr = failure
	if err := runner.Deploy(ctx, request); !errors.Is(err, failure) {
		t.Fatalf("Deploy error = %v, want %v", err, failure)
	}
}

func TestFakeRunnerRestartStatusAndLogs(t *testing.T) {
	ctx := context.Background()
	runner := &FakeRunner{
		Services: []ServiceStatus{
			{Name: "web", State: "running"},
			{Name: "worker", State: "exited"},
		},
		LogOutput: "web log line\n",
	}

	restart := RestartRequest{AppName: "my-app", ServiceName: "web"}
	if err := runner.Restart(ctx, restart); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if runner.RestartRequests[0] != restart {
		t.Fatalf("RestartRequests = %#v", runner.RestartRequests)
	}

	statuses, err := runner.Status(ctx, StatusRequest{AppName: "my-app"})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statuses) != 2 || statuses[0].Name != "web" || statuses[1].State != "exited" {
		t.Fatalf("Status = %#v", statuses)
	}

	logs, err := runner.Logs(ctx, LogsRequest{AppName: "my-app", ServiceName: "web", Lines: 100})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if logs != "web log line\n" {
		t.Fatalf("Logs = %q", logs)
	}
}

func TestFakeRunnerRestartStatusAndLogsFailures(t *testing.T) {
	ctx := context.Background()
	restartFailure := errors.New("restart failed")
	statusFailure := errors.New("status failed")
	logsFailure := errors.New("logs failed")
	runner := &FakeRunner{
		RestartErr: restartFailure,
		StatusErr:  statusFailure,
		LogsErr:    logsFailure,
	}

	if err := runner.Restart(ctx, RestartRequest{}); !errors.Is(err, restartFailure) {
		t.Fatalf("Restart error = %v, want %v", err, restartFailure)
	}
	if _, err := runner.Status(ctx, StatusRequest{}); !errors.Is(err, statusFailure) {
		t.Fatalf("Status error = %v, want %v", err, statusFailure)
	}
	if _, err := runner.Logs(ctx, LogsRequest{}); !errors.Is(err, logsFailure) {
		t.Fatalf("Logs error = %v, want %v", err, logsFailure)
	}
}

package compose

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDockerRunnerStartAndStopUseLiteralComposeCommands(t *testing.T) {
	// Given
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	executor := &recordingExecutor{}
	runner := NewDockerRunner(executor)
	env := map[string]string{"APP_MESSAGE": "configured"}
	request := LifecycleRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath, Env: env}

	// When
	if err := runner.Start(ctx, request); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := runner.Stop(ctx, request); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Then
	want := []Command{
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "start"}, Env: env},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "stop"}, Env: env},
	}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}
}

func TestFakeRunnerStartAndStopCaptureRequestsAndFailures(t *testing.T) {
	// Given
	ctx := context.Background()
	startFailure := errors.New("start failed")
	stopFailure := errors.New("stop failed")
	runner := &FakeRunner{StartErr: startFailure, StopErr: stopFailure}
	request := LifecycleRequest{AppName: "my-app", ComposePath: "/apps/my-app/compose.yml"}

	// When
	startErr := runner.Start(ctx, request)
	stopErr := runner.Stop(ctx, request)

	// Then
	if !errors.Is(startErr, startFailure) {
		t.Fatalf("Start error = %v, want %v", startErr, startFailure)
	}
	if !errors.Is(stopErr, stopFailure) {
		t.Fatalf("Stop error = %v, want %v", stopErr, stopFailure)
	}
	if !reflect.DeepEqual(runner.StartRequests, []LifecycleRequest{request}) {
		t.Fatalf("StartRequests = %#v", runner.StartRequests)
	}
	if !reflect.DeepEqual(runner.StopRequests, []LifecycleRequest{request}) {
		t.Fatalf("StopRequests = %#v", runner.StopRequests)
	}
}

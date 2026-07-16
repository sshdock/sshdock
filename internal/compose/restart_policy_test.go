package compose

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNonRestartingServicesReportsCandidatesUsingDefaultPolicy(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	content := `services:
  web:
    image: example/web
    restart: unless-stopped
  worker:
    image: example/worker
  scheduler:
    image: example/scheduler
    restart: "no"
  api:
    image: example/api
    restart: ${API_RESTART:-no}
`
	if err := os.WriteFile(composePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write Compose file: %v", err)
	}

	// When
	services, err := NonRestartingServices(composePath, map[string]string{"API_RESTART": "always"}, []string{"scheduler", "web", "worker", "api", "missing"})

	// Then
	if err != nil {
		t.Fatalf("NonRestartingServices: %v", err)
	}
	want := []string{"scheduler", "worker"}
	if !reflect.DeepEqual(services, want) {
		t.Fatalf("services = %#v, want %#v", services, want)
	}
}

func TestDockerRunnerNonRestartingServicesUsesEffectiveComposeModel(t *testing.T) {
	// Given
	executor := &recordingExecutor{Outputs: []string{`{"services":{"web":{"restart":"unless-stopped"},"worker":{"restart":"no"}}}`}}
	runner := NewDockerRunner(executor)
	request := StatusRequest{AppName: "my-app", ProjectDir: "/apps/my-app/worktree", ComposePath: "/apps/my-app/worktree/compose.yml", Env: map[string]string{"TOKEN": "secret"}}

	// When
	services, err := runner.NonRestartingServices(context.Background(), request, []string{"web", "worker"})

	// Then
	if err != nil {
		t.Fatalf("NonRestartingServices: %v", err)
	}
	if !reflect.DeepEqual(services, []string{"worker"}) {
		t.Fatalf("services = %#v, want worker", services)
	}
	wantArgs := []string{"compose", "-f", request.ComposePath, "-p", "sshdock_my-app", "config", "--format", "json"}
	if len(executor.Commands) != 1 || !reflect.DeepEqual(executor.Commands[0].Args, wantArgs) || executor.Commands[0].Dir != request.ProjectDir || !reflect.DeepEqual(executor.Commands[0].Env, request.Env) {
		t.Fatalf("commands = %#v, want effective Compose config command", executor.Commands)
	}
}

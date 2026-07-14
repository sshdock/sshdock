package compose

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDockerRunnerValidatePassesStandardFieldsToDockerCompose(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yaml")
	writeFile(t, composePath, `services:
  web:
    image: example/web:latest
    command: ["./serve"]
    labels:
      com.example.role: web
    networks: [frontend]
    configs: [app-config]
    secrets: [app-secret]
    deploy:
      resources:
        limits:
          memory: 256M
networks:
  frontend: {}
configs:
  app-config:
    file: ./app.conf
secrets:
  app-secret:
    file: ./app.secret
`)
	executor := &recordingExecutor{}
	runner := NewDockerRunner(executor)

	// When
	_, err := runner.Validate(context.Background(), "my-app", composePath)

	// Then
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	want := []Command{{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "config"}}}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}
}

func TestDockerRunnerValidateUsesStableSSHDockProjectName(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, "name: caller-controlled\nservices:\n  web:\n    image: example/web:latest\n")
	executor := &recordingExecutor{}
	runner := NewDockerRunner(executor)

	// When
	_, err := runner.Validate(context.Background(), "my-app", composePath)

	// Then
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	want := []Command{{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "config"}}}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}
}

func TestDockerRunnerValidateRejectsExternalFilesBeforeDockerCompose(t *testing.T) {
	// Given
	composePath := filepath.Join(t.TempDir(), "compose.yml")
	writeFile(t, composePath, "include: shared.compose.yml\nservices:\n  web:\n    image: example/web:latest\n")
	executor := &recordingExecutor{}
	runner := NewDockerRunner(executor)

	// When
	_, err := runner.Validate(context.Background(), "my-app", composePath)

	// Then
	if err == nil || !strings.Contains(err.Error(), "external Compose files are not supported") {
		t.Fatalf("Validate error = %q, want external file rejection", err)
	}
	if len(executor.Commands) != 0 {
		t.Fatalf("commands = %#v, want no Docker Compose execution", executor.Commands)
	}
}

func TestDockerRunnerDeployUsesStableSSHDockProjectName(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, "name: caller-controlled\nservices:\n  web:\n    image: example/web:latest\n")
	executor := &recordingExecutor{}
	runner := NewDockerRunner(executor)

	// When
	err := runner.Deploy(context.Background(), DeployRequest{
		AppName:     "my-app",
		ProjectDir:  projectDir,
		ComposePath: composePath,
	})

	// Then
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	for _, command := range executor.Commands {
		if command.Name != "docker" || len(command.Args) < 2 || command.Args[0] != "compose" {
			continue
		}
		if !containsArgs(command.Args, "-p", "sshdock_my-app") {
			t.Fatalf("Compose command project = %#v, want stable SSHDock project name", command.Args)
		}
	}
}

func TestDockerRunnerDeployBuildsServiceInheritedFromSameFileAnchor(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `
x-build: &build
  build: .
services:
  web:
    <<: *build
`)
	executor := &recordingExecutor{Outputs: []string{"web\n"}}
	runner := NewDockerRunner(executor)

	// When
	err := runner.Deploy(context.Background(), DeployRequest{
		AppName:     "my-app",
		ProjectDir:  projectDir,
		ComposePath: composePath,
		CommitSHA:   "abc123",
	})

	// Then
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	foundBuild := false
	for _, command := range executor.Commands {
		if containsArgs(command.Args, "build", "web") {
			foundBuild = true
			break
		}
	}
	if !foundBuild {
		t.Fatalf("commands = %#v, want anchored web build", executor.Commands)
	}
}

func TestDockerRunnerDeployBuildsServiceInheritedFromSameFileExtends(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `
services:
  base:
    build: .
  web:
    extends:
      service: base
`)
	executor := &recordingExecutor{Outputs: []string{"base\nweb\n"}}
	runner := NewDockerRunner(executor)

	// When
	err := runner.Deploy(context.Background(), DeployRequest{
		AppName:     "my-app",
		ProjectDir:  projectDir,
		ComposePath: composePath,
		CommitSHA:   "abc123",
	})

	// Then
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	for _, command := range executor.Commands {
		if containsArgAfter(command.Args, "build", "web") {
			return
		}
	}
	t.Fatalf("commands = %#v, want same-file extended web build", executor.Commands)
}

func TestDockerRunnerDeployDoesNotActivateProfiledBuildService(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `
services:
  web:
    image: example/web:latest
  debug:
    profiles: [debug]
    build: .
`)
	executor := &recordingExecutor{Outputs: []string{"web\n"}}
	runner := NewDockerRunner(executor)

	// When
	err := runner.Deploy(context.Background(), DeployRequest{
		AppName:     "my-app",
		ProjectDir:  projectDir,
		ComposePath: composePath,
		CommitSHA:   "abc123",
	})

	// Then
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	for _, command := range executor.Commands {
		if containsArgAfter(command.Args, "build", "debug") || strings.Contains(strings.Join(command.Args, " "), "sshdock/my-app/debug") {
			t.Fatalf("inactive profiled service was built or tagged: %#v", command)
		}
	}
}

func containsArgAfter(args []string, marker string, value string) bool {
	markerFound := false
	for _, arg := range args {
		if markerFound && arg == value {
			return true
		}
		if arg == marker {
			markerFound = true
		}
	}
	return false
}

func containsArgs(args []string, first string, second string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == first && args[index+1] == second {
			return true
		}
	}
	return false
}

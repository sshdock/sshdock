package compose

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDockerRunnerDeployBuildsServiceWithInterpolatedSameFileExtends(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, `
services:
  base:
    build: .
  web:
    extends:
      service: ${BASE_SERVICE:-base}
`)
	executor := &recordingExecutor{Outputs: []string{`{"services":{"base":{"build":{"context":"."}},"web":{"build":{"context":"."}}}}`}}
	runner := NewDockerRunner(executor)

	// When
	_, err := runner.Deploy(context.Background(), DeployRequest{
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
		if containsArgAfter(command.Args, "compose", "build") && !containsArgAfter(command.Args, "build", "web") {
			return
		}
	}
	t.Fatalf("commands = %#v, want interpolated extended web build", executor.Commands)
}

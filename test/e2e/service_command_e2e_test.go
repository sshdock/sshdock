//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestDockerServiceCommandsEndToEnd(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run Docker service command tests")
	}
	requireDocker(t)

	// Given
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	appName := "service-command-" + strings.ToLower(time.Now().UTC().Format("150405"))
	contents := "services:\n  web:\n    image: " + lifecycleBusyboxImage + "\n    command: [\"sleep\", \"300\"]\n"
	if err := os.WriteFile(composePath, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	runner := compose.NewDockerRunner(compose.LocalCommandExecutor{})
	if _, err := runner.Deploy(ctx, compose.DeployRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	t.Cleanup(func() {
		_ = runner.Remove(context.Background(), compose.RemoveRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath})
	})
	command := []string{"sh", "-c", `printf '%s' "$1"`, "_", "value with spaces"}
	var execOutput bytes.Buffer
	var execErrorOutput bytes.Buffer
	var runOutput bytes.Buffer
	var runErrorOutput bytes.Buffer

	// When
	if err := runner.Exec(ctx, compose.ServiceCommandRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath, ServiceName: "web", Command: command, Stdout: &execOutput, Stderr: &execErrorOutput}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if err := runner.RunOneOff(ctx, compose.ServiceCommandRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath, ServiceName: "web", Command: command, Stdout: &runOutput, Stderr: &runErrorOutput}); err != nil {
		t.Fatalf("RunOneOff: %v", err)
	}

	// Then
	if execOutput.String() != "value with spaces" || runOutput.String() != "value with spaces" {
		t.Fatalf("service command output: exec=%q run=%q", execOutput.String(), runOutput.String())
	}
	containerIDs, err := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+compose.ProjectName(appName), "--filter", "label=com.docker.compose.oneoff=True").CombinedOutput()
	if err != nil {
		t.Fatalf("inspect one-off containers: %v\n%s", err, containerIDs)
	}
	if strings.TrimSpace(string(containerIDs)) != "" {
		t.Fatalf("one-off container was not removed: %s", containerIDs)
	}
	if err := runner.Exec(ctx, compose.ServiceCommandRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath, ServiceName: "missing", Command: []string{"true"}}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "service") {
		t.Fatalf("missing service error = %v", err)
	}
	if err := runner.Stop(ctx, compose.LifecycleRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath}); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := runner.Exec(ctx, compose.ServiceCommandRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath, ServiceName: "web", Command: []string{"true"}}); err == nil {
		t.Fatal("exec against stopped service succeeded")
	}
}

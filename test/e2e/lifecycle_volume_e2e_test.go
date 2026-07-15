//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

const lifecycleBusyboxImage = "busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662"

func TestDockerLifecyclePreservesExistingConfigAndNamedVolume(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run Docker lifecycle volume tests")
	}
	requireDocker(t)

	// Given
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	appName := "lifecycle-volume-" + strings.ToLower(time.Now().UTC().Format("150405"))
	writeLifecycleCompose(t, composePath)
	runner := compose.NewDockerRunner(compose.LocalCommandExecutor{})
	deployRequest := compose.DeployRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath, Env: map[string]string{"VALUE": "old"}}
	if _, err := runner.Deploy(ctx, deployRequest); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	volumeName := compose.ProjectName(appName) + "_data"
	t.Cleanup(func() {
		_ = runner.Remove(context.Background(), compose.RemoveRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath, Env: map[string]string{"VALUE": "new"}})
		_ = exec.Command("docker", "volume", "rm", "-f", volumeName).Run()
	})

	// When
	lifecycleRequest := compose.LifecycleRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath, Env: map[string]string{"VALUE": "new"}}
	if err := runner.Stop(ctx, lifecycleRequest); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := runner.Start(ctx, lifecycleRequest); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := runner.Restart(ctx, compose.RestartRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath, Env: map[string]string{"VALUE": "new"}}); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if err := runner.Remove(ctx, compose.RemoveRequest{AppName: appName, ProjectDir: projectDir, ComposePath: composePath, Env: map[string]string{"VALUE": "new"}}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Then
	if output, err := exec.CommandContext(ctx, "docker", "volume", "inspect", volumeName).CombinedOutput(); err != nil {
		t.Fatalf("named volume was removed: %v\n%s", err, output)
	}
	output, err := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", volumeName+":/data", lifecycleBusyboxImage, "cat", "/data/value").CombinedOutput()
	if err != nil {
		t.Fatalf("read preserved volume: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "old" {
		t.Fatalf("preserved value = %q, want old; restart/start applied changed Compose config", strings.TrimSpace(string(output)))
	}
}

func writeLifecycleCompose(t *testing.T, path string) {
	t.Helper()
	contents := "services:\n  web:\n    image: " + lifecycleBusyboxImage + "\n    environment:\n      VALUE: ${VALUE:?VALUE is required}\n    command: [\"sh\", \"-c\", \"printf '%s' \\\"$$VALUE\\\" > /data/value; exec sleep 300\"]\n    volumes:\n      - data:/data\nvolumes:\n  data:\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
}

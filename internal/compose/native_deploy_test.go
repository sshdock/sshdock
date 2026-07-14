package compose

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDockerRunnerDeployUsesNativeComposeModelAndWaitsForHealth(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, "services:\n  web:\n    build: .\n    ports: [\"127.0.0.1:3100:3000\"]\n")
	executor := &recordingExecutor{Outputs: []string{`{
  "services": {
    "web": {
      "build": {"context": "."},
      "ports": [{"host_ip": "127.0.0.1", "published": "3100", "target": 3000, "protocol": "tcp"}]
    }
  }
}`}}
	runner := NewDockerRunner(executor)

	// When
	result, err := runner.Deploy(context.Background(), DeployRequest{
		AppName:     "my-app",
		ProjectDir:  projectDir,
		ComposePath: composePath,
		CommitSHA:   "abc123",
	})

	// Then
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !result.RouteFound || result.RouteTarget != (RouteTarget{ServiceName: "web", Port: 3100}) {
		t.Fatalf("route result = %#v, want web:3100", result)
	}
	want := []Command{
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "config", "--format", "json"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "pull", "--ignore-buildable"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "build"}},
		{Name: "docker", Dir: projectDir, Args: []string{"compose", "-f", composePath, "-p", "sshdock_my-app", "up", "-d", "--wait", "--wait-timeout", "120"}},
	}
	if !reflect.DeepEqual(executor.Commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.Commands, want)
	}
	if wantDeadlines := []bool{false, false, false, true}; !reflect.DeepEqual(executor.Deadlines, wantDeadlines) {
		t.Fatalf("command deadlines = %#v, want %#v", executor.Deadlines, wantDeadlines)
	}
	matches, err := filepath.Glob(filepath.Join(projectDir, ".sshdock", "*.compose.yml"))
	if err != nil {
		t.Fatalf("Glob generated overrides: %v", err)
	} else if len(matches) != 0 {
		t.Fatalf("generated release overrides = %#v, want none", matches)
	}
	for _, command := range executor.Commands {
		joined := strings.Join(command.Args, " ")
		if strings.Contains(joined, "image tag") || strings.Contains(joined, "image rm") || strings.Contains(joined, "sshdock/my-app/") {
			t.Fatalf("unexpected SSHDock image management: %#v", command)
		}
	}
}

func TestDockerRunnerDeployReportsEffectiveModelWarnings(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, "services:\n  web:\n    image: example/web:latest\n")
	executor := &recordingExecutor{Outputs: []string{`{
  "services": {
    "web": {
      "image": "example/web:latest",
      "privileged": true,
      "network_mode": "host",
      "ports": [
        {"host_ip": "0.0.0.0", "published": "8080", "target": 80, "protocol": "tcp"},
        {"host_ip": "::", "published": "5353", "target": 53, "protocol": "udp"}
      ],
      "volumes": [
        {"type": "bind", "source": "/var/run/docker.sock", "target": "/var/run/docker.sock"},
        {"type": "bind", "source": "/srv/data", "target": "/data"}
      ]
    }
  },
  "volumes": {
    "global": {"name": "shared-global"},
    "external": {"name": "external-data", "external": true}
  }
}`}}
	runner := NewDockerRunner(executor)

	// When
	result, err := runner.Deploy(context.Background(), DeployRequest{
		AppName:     "my-app",
		ProjectDir:  projectDir,
		ComposePath: composePath,
	})

	// Then
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	for _, want := range []string{
		"publishes 0.0.0.0:8080 on all interfaces",
		"publishes :::5353 on all interfaces",
		"uses privileged mode",
		"uses host networking",
		"mounts the Docker socket",
		"uses host bind mount /srv/data",
		"uses explicit volume name shared-global",
		"uses external volume external-data",
	} {
		if !containsWarning(result.Warnings, want) {
			t.Fatalf("warnings = %#v, want substring %q", result.Warnings, want)
		}
	}
}

func TestDockerRunnerDeployPrefersLoopbackRouteCandidate(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	writeFile(t, composePath, "services:\n  api:\n    image: example/api\n  metrics:\n    image: example/metrics\n")
	executor := &recordingExecutor{Outputs: []string{`{
  "services": {
    "api": {"ports": [{"host_ip": "127.0.0.1", "published": "3100", "target": 3000, "protocol": "tcp"}]},
    "metrics": {"ports": [{"host_ip": "0.0.0.0", "published": "3200", "target": 3000, "protocol": "tcp"}]}
  }
}`}}
	runner := NewDockerRunner(executor)

	// When
	result, err := runner.Deploy(context.Background(), DeployRequest{AppName: "my-app", ProjectDir: projectDir, ComposePath: composePath})

	// Then
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	want := RouteTarget{ServiceName: "api", Port: 3100}
	if !result.RouteFound || result.RouteTarget != want {
		t.Fatalf("route = %#v, want %#v", result, want)
	}
}

func containsWarning(warnings []string, substring string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, substring) {
			return true
		}
	}
	return false
}

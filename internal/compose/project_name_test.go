package compose

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/domain"
)

func TestProjectNamePreservesValidAppIdentity(t *testing.T) {
	seen := map[string]string{}
	for _, name := range []string{"a", "0", "my-app", "my--app", "a-b", "ab", "a0", "0a", strings.Repeat("a", 63)} {
		project, err := ProjectName(name)
		if err != nil || project != "sshdock_"+name {
			t.Fatalf("ProjectName(%q) = %q, %v", name, project, err)
		}
		if earlier, found := seen[project]; found {
			t.Fatalf("apps %q and %q share project %q", earlier, name, project)
		}
		seen[project] = name
	}
}

func TestDockerRunnerRejectsInvalidAppNamesBeforeAnyCommand(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	composePath := filepath.Join(projectDir, "compose.yml")
	// An invalid app name must fail even before looking for a Compose file.
	for _, name := range []string{"", "My-App", "my_app", "my.app", " my-app", "my-app ", "-my-app", "my-app-", "my app", "../escape", strings.Repeat("a", 64)} {
		for _, operation := range []struct {
			name string
			run  func(*DockerRunner) error
		}{
			{"validate", func(r *DockerRunner) error { _, err := r.Validate(ctx, name, composePath); return err }},
			{"deploy", func(r *DockerRunner) error {
				_, err := r.Deploy(ctx, DeployRequest{AppName: name, ProjectDir: projectDir, ComposePath: composePath})
				return err
			}},
			{"deploy with output", func(r *DockerRunner) error {
				_, err := r.DeployWithOutput(ctx, DeployRequest{AppName: name, ProjectDir: projectDir, ComposePath: composePath}, io.Discard, io.Discard)
				return err
			}},
			{"start", func(r *DockerRunner) error { return r.Start(ctx, LifecycleRequest{AppName: name}) }},
			{"stop", func(r *DockerRunner) error { return r.Stop(ctx, LifecycleRequest{AppName: name}) }},
			{"restart", func(r *DockerRunner) error { return r.Restart(ctx, RestartRequest{AppName: name}) }},
			{"remove", func(r *DockerRunner) error { return r.Remove(ctx, RemoveRequest{AppName: name}) }},
			{"status", func(r *DockerRunner) error { _, err := r.Status(ctx, StatusRequest{AppName: name}); return err }},
			{"logs", func(r *DockerRunner) error { _, err := r.Logs(ctx, LogsRequest{AppName: name}); return err }},
			{"stream logs", func(r *DockerRunner) error {
				return r.StreamLogs(ctx, LogsRequest{AppName: name}, io.Discard, io.Discard)
			}},
			{"exec", func(r *DockerRunner) error {
				return r.Exec(ctx, ServiceCommandRequest{AppName: name, ServiceName: "web", Command: []string{"true"}})
			}},
			{"run", func(r *DockerRunner) error {
				return r.RunOneOff(ctx, ServiceCommandRequest{AppName: name, ServiceName: "web", Command: []string{"true"}})
			}},
			{"restart policies", func(r *DockerRunner) error {
				_, err := r.NonRestartingServices(ctx, StatusRequest{AppName: name}, []string{"web"})
				return err
			}},
			{"server preflight", func(r *DockerRunner) error {
				_, err := r.Preflight(ctx, PreflightRequest{AppName: name, ProjectDir: projectDir, Scope: "server"})
				return err
			}},
			{"local preflight", func(r *DockerRunner) error {
				_, err := r.Preflight(ctx, PreflightRequest{AppName: name, ProjectDir: projectDir, Scope: "local"})
				return err
			}},
		} {
			t.Run(name+"/"+operation.name, func(t *testing.T) {
				executor := &projectBoundaryExecutor{}
				err := operation.run(NewDockerRunner(executor))
				var invalid *domain.InvalidAppNameError
				if !errors.As(err, &invalid) || invalid.Name != name {
					t.Fatalf("error = %v, want invalid app name %q", err, name)
				}
				if len(executor.commands) != 0 {
					t.Fatalf("invalid name reached executor: %#v", executor.commands)
				}
			})
		}
	}
}

func TestLocalPreflightProjectDoesNotDependOnDirectoryBasename(t *testing.T) {
	request, executor := preflightFixture(t, "local")
	request.AppName = "preflight"
	request.ProjectDir = filepath.Join(t.TempDir(), "Mixed.Case_directory with spaces")
	if err := os.MkdirAll(request.ProjectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(request.ProjectDir, "compose.yml"), "name: caller-controlled\nservices:\n  web:\n    image: example/web\n")
	report, err := NewDockerRunner(executor).Preflight(context.Background(), request)
	if err != nil || !report.OK {
		t.Fatalf("local source check = %#v, %v", report, err)
	}
	if len(executor.commands) != 3 || !containsArgs(executor.commands[2].Args, "-p", "sshdock_preflight") {
		t.Fatalf("local preflight commands = %#v", executor.commands)
	}
}

type projectBoundaryExecutor struct{ commands []Command }

func (e *projectBoundaryExecutor) Run(_ context.Context, command Command) (string, error) {
	e.commands = append(e.commands, command)
	return "", nil
}

func (e *projectBoundaryExecutor) Stream(_ context.Context, command Command, _, _ io.Writer) error {
	e.commands = append(e.commands, command)
	return nil
}

func (e *projectBoundaryExecutor) RunAttached(_ context.Context, command Command, _ io.Reader, _, _ io.Writer) error {
	e.commands = append(e.commands, command)
	return nil
}

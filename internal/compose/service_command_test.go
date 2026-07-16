package compose

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
)

func TestDockerRunnerServiceCommandsPreserveArgvAndSelectTTYMode(t *testing.T) {
	// Given
	ctx := context.Background()
	executor := &recordingAttachedExecutor{}
	runner := NewDockerRunner(executor)
	stdin := bytes.NewBufferString("input\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := []string{"sh", "-c", `printf '%s\n' "$1"`, "_", "value with spaces"}
	base := ServiceCommandRequest{
		AppName:     "my-app",
		ProjectDir:  "/apps/my-app/worktree",
		ComposePath: "/apps/my-app/worktree/compose.yml",
		ServiceName: "web",
		Command:     command,
		Stdin:       stdin,
		Stdout:      &stdout,
		Stderr:      &stderr,
	}

	// When
	execRequest := base
	execRequest.TTY = true
	if err := runner.Exec(ctx, execRequest); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	runRequest := base
	runRequest.TTY = false
	if err := runner.RunOneOff(ctx, runRequest); err != nil {
		t.Fatalf("RunOneOff: %v", err)
	}

	// Then
	want := []Command{
		{Name: "docker", Dir: base.ProjectDir, Args: []string{"compose", "-f", base.ComposePath, "-p", "sshdock_my-app", "exec", "--", "web", "sh", "-c", `printf '%s\n' "$1"`, "_", "value with spaces"}},
		{Name: "docker", Dir: base.ProjectDir, Args: []string{"compose", "-f", base.ComposePath, "-p", "sshdock_my-app", "run", "--rm", "-T", "--", "web", "sh", "-c", `printf '%s\n' "$1"`, "_", "value with spaces"}},
	}
	if !reflect.DeepEqual(executor.commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.commands, want)
	}
	for index, streams := range executor.streams {
		if streams.stdin != stdin || streams.stdout != &stdout || streams.stderr != &stderr {
			t.Fatalf("streams[%d] = %#v, want original streams", index, streams)
		}
	}
}

type recordingAttachedExecutor struct {
	commands []Command
	streams  []attachedStreams
}

type attachedStreams struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func (e *recordingAttachedExecutor) Run(context.Context, Command) (string, error) {
	return "", nil
}

func (e *recordingAttachedExecutor) RunAttached(_ context.Context, command Command, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	e.commands = append(e.commands, command)
	e.streams = append(e.streams, attachedStreams{stdin: stdin, stdout: stdout, stderr: stderr})
	return nil
}

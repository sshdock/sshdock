package compose

import (
	"context"
	"fmt"
	"io"
)

type attachedCommandExecutor interface {
	RunAttached(ctx context.Context, command Command, stdin io.Reader, stdout io.Writer, stderr io.Writer) error
}

type serviceCommandAction string

const (
	serviceCommandExec serviceCommandAction = "exec"
	serviceCommandRun  serviceCommandAction = "run"
)

func (r *DockerRunner) Exec(ctx context.Context, request ServiceCommandRequest) error {
	return r.runServiceCommand(ctx, serviceCommandExec, request)
}

func (r *DockerRunner) RunOneOff(ctx context.Context, request ServiceCommandRequest) error {
	return r.runServiceCommand(ctx, serviceCommandRun, request)
}

func (r *DockerRunner) runServiceCommand(ctx context.Context, action serviceCommandAction, request ServiceCommandRequest) error {
	if request.ServiceName == "" {
		return fmt.Errorf("service name is required")
	}
	if len(request.Command) == 0 {
		return fmt.Errorf("service command is required")
	}
	executor, ok := r.executor.(attachedCommandExecutor)
	if !ok {
		return fmt.Errorf("attached Compose command execution is not configured")
	}

	args := commandArgs(composeArgs([]string{request.ComposePath}, ProjectName(request.AppName)), string(action))
	if action == serviceCommandRun {
		args = append(args, "--rm")
	}
	if !request.TTY {
		args = append(args, "-T")
	}
	args = append(args, "--", request.ServiceName)
	args = append(args, request.Command...)
	return executor.RunAttached(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: args, Env: request.Env}, request.Stdin, request.Stdout, request.Stderr)
}

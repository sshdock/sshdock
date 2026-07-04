package compose

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type LocalCommandExecutor struct{}

func (LocalCommandExecutor) Run(ctx context.Context, command Command) (string, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %w\n%s", command.Name, strings.Join(command.Args, " "), err, output)
	}

	return string(output), nil
}

func (LocalCommandExecutor) Stream(ctx context.Context, command Command, stdout io.Writer, stderr io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w", command.Name, strings.Join(command.Args, " "), err)
	}
	return nil
}

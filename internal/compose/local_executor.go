package compose

import (
	"context"
	"fmt"
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

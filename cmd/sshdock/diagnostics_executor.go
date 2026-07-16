package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sshdock/sshdock/internal/diagnostics"
)

type diagnosticsLocalExecutor struct{}

func (diagnosticsLocalExecutor) Run(ctx context.Context, command diagnostics.Command) (string, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = os.Environ()
	for key, value := range command.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if command.Dir != "" {
		cmd.Env = append(cmd.Env, "PWD="+command.Dir)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w: %s", command.Name, strings.Join(command.Args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

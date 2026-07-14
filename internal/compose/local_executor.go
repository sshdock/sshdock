package compose

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type LocalCommandExecutor struct{}

func (LocalCommandExecutor) Run(ctx context.Context, command Command) (string, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = commandEnv(command.Env, command.Dir)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		detail := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
		return "", fmt.Errorf("%s %s failed: %w\n%s", command.Name, strings.Join(command.Args, " "), err, detail)
	}

	return stdout.String(), nil
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
	cmd.Env = commandEnv(command.Env, command.Dir)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w", command.Name, strings.Join(command.Args, " "), err)
	}
	return nil
}

func commandEnv(extra map[string]string, directory string) []string {
	env := os.Environ()
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	if directory != "" {
		env = append(env, "PWD="+directory)
	}
	return env
}

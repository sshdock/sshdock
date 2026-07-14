package compose

import (
	"context"
	"path/filepath"
)

func (r *DockerRunner) Validate(ctx context.Context, appName string, composePath string) (ValidationResult, error) {
	result, err := ValidateFile(composePath)
	if err != nil {
		return ValidationResult{}, err
	}
	command := Command{
		Name: "docker",
		Dir:  filepath.Dir(composePath),
		Args: commandArgs(composeArgs([]string{composePath}, ProjectName(appName)), "config"),
	}
	if _, err := r.executor.Run(ctx, command); err != nil {
		return ValidationResult{}, err
	}

	return result, nil
}

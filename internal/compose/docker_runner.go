package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	domaincfg "github.com/sshdock/sshdock/internal/domain"
)

const (
	defaultDeployWait     = 2 * time.Minute
	defaultDeployHostWait = defaultDeployWait + 10*time.Second
)

type DockerRunner struct {
	executor CommandExecutor
}

type streamingCommandExecutor interface {
	Stream(ctx context.Context, command Command, stdout io.Writer, stderr io.Writer) error
}

func NewDockerRunner(executor CommandExecutor) *DockerRunner {
	return &DockerRunner{executor: executor}
}

func (r *DockerRunner) Deploy(ctx context.Context, request DeployRequest) (DeployResult, error) {
	return r.deploy(ctx, request, io.Discard, io.Discard)
}

func (r *DockerRunner) DeployWithOutput(ctx context.Context, request DeployRequest, stdout io.Writer, stderr io.Writer) (DeployResult, error) {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return r.deploy(ctx, request, stdout, stderr)
}

func (r *DockerRunner) deploy(ctx context.Context, request DeployRequest, stdout io.Writer, stderr io.Writer) (DeployResult, error) {
	projectName := request.projectName()
	baseArgs := composeArgs([]string{request.ComposePath}, projectName)

	if _, err := fmt.Fprintln(stdout, "stage: validate compose"); err != nil {
		return DeployResult{}, NewDeployError(DeployStageValidateCompose, err)
	}
	if _, err := ValidateFileWithEnv(request.ComposePath, request.Env); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return DeployResult{}, NewDeployError(DeployStageValidateCompose, err)
	}
	if _, err := fmt.Fprintln(stdout, "stage: evaluate effective model"); err != nil {
		return DeployResult{}, NewDeployError(DeployStageComposeConfig, err)
	}
	effectiveOutput, err := r.runDeploymentCommand(ctx, deployCommand(request, commandArgs(baseArgs, "config", "--format", "json")), stdout, stderr)
	if err != nil {
		return DeployResult{}, NewDeployError(DeployStageComposeConfig, err)
	}
	result, err := analyzeEffectiveModel(effectiveOutput, projectName)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return DeployResult{}, NewDeployError(DeployStageComposeConfig, err)
	}
	if _, err := fmt.Fprintln(stdout, "stage: pull images"); err != nil {
		return result, NewDeployError(DeployStagePullImages, err)
	}
	if _, err := r.runDeploymentCommand(ctx, deployCommand(request, commandArgs(baseArgs, "pull", "--ignore-buildable")), stdout, stderr); err != nil {
		return result, NewDeployError(DeployStagePullImages, err)
	}
	if _, err := fmt.Fprintln(stdout, "stage: build services"); err != nil {
		return result, NewDeployError(DeployStageBuildServices, err)
	}
	if _, err := r.runDeploymentCommand(ctx, deployCommand(request, commandArgs(baseArgs, "build")), stdout, stderr); err != nil {
		return result, NewDeployError(DeployStageBuildServices, err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, defaultDeployHostWait)
	defer cancel()
	waitSeconds := strconv.FormatInt(int64(defaultDeployWait/time.Second), 10)
	upArgs := commandArgs(baseArgs, "up", "-d", "--wait", "--wait-timeout", waitSeconds)
	if _, err := fmt.Fprintln(stdout, "stage: start services"); err != nil {
		return result, NewDeployError(DeployStageWaitServices, err)
	}
	if _, err := r.runDeploymentCommand(waitCtx, deployCommand(request, upArgs), stdout, stderr); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = fmt.Errorf("Compose service wait interrupted by deployment context: %w", ctxErr)
		} else if waitCtx.Err() != nil {
			err = fmt.Errorf("Compose service wait exceeded %s: %w", defaultDeployWait, waitCtx.Err())
		}
		return result, NewDeployError(DeployStageWaitServices, err)
	}

	return result, nil
}

func (r *DockerRunner) runDeploymentCommand(ctx context.Context, command Command, stdout io.Writer, stderr io.Writer) (string, error) {
	if streamer, ok := r.executor.(streamingCommandExecutor); ok {
		var captured bytes.Buffer
		if err := streamer.Stream(ctx, command, io.MultiWriter(stdout, &captured), stderr); err != nil {
			return "", err
		}
		return captured.String(), nil
	}
	output, err := r.executor.Run(ctx, command)
	if output != "" {
		if _, writeErr := io.WriteString(stdout, output); writeErr != nil {
			return "", writeErr
		}
	}
	if err != nil {
		if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
			return "", errors.Join(err, writeErr)
		}
		return "", err
	}
	return output, nil
}

func (r *DockerRunner) Restart(ctx context.Context, request RestartRequest) error {
	args := commandArgs(composeArgs([]string{request.ComposePath}, request.projectName()), "restart")
	if request.ServiceName != "" {
		args = append(args, request.ServiceName)
	}

	_, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: args, Env: request.Env})
	return err
}

func (r *DockerRunner) Remove(ctx context.Context, request RemoveRequest) error {
	args := commandArgs(composeArgs([]string{request.ComposePath}, request.projectName()), "down", "--remove-orphans")
	_, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: args, Env: request.Env})
	return err
}

func (r *DockerRunner) Status(ctx context.Context, request StatusRequest) ([]ServiceStatus, error) {
	args := commandArgs(composeArgs([]string{request.ComposePath}, request.projectName()), "ps", "--all", "--format", "json")
	output, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: args, Env: request.Env})
	if err != nil {
		return nil, err
	}

	return parseServiceStatuses(output)
}

func (r *DockerRunner) Logs(ctx context.Context, request LogsRequest) (string, error) {
	return r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: logsArgs(request), Env: request.Env})
}

func (r *DockerRunner) StreamLogs(ctx context.Context, request LogsRequest, stdout io.Writer, stderr io.Writer) error {
	if streamer, ok := r.executor.(streamingCommandExecutor); ok {
		return streamer.Stream(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: logsArgs(request), Env: request.Env}, stdout, stderr)
	}
	output, err := r.Logs(ctx, request)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(stdout, output)
	return err
}

func composeArgs(composeFiles []string, projectName string) []string {
	args := []string{"compose"}
	for _, composeFile := range composeFiles {
		args = append(args, "-f", composeFile)
	}
	if projectName != "" {
		args = append(args, "-p", projectName)
	}
	return args
}

func deployCommand(request DeployRequest, args []string) Command {
	return Command{Name: "docker", Dir: request.ProjectDir, Args: args, Env: request.Env}
}

func commandArgs(base []string, tail ...string) []string {
	args := append([]string{}, base...)
	return append(args, tail...)
}

func logsArgs(request LogsRequest) []string {
	args := commandArgs(composeArgs([]string{request.ComposePath}, request.projectName()), "logs")
	if request.Follow {
		args = append(args, "--follow")
	}
	if request.Lines > 0 {
		args = append(args, "--tail", strconv.Itoa(request.Lines))
	}
	if request.ServiceName != "" {
		args = append(args, request.ServiceName)
	}
	return args
}

func (r DeployRequest) projectName() string {
	return ProjectName(r.AppName)
}

func (r RestartRequest) projectName() string {
	return ProjectName(r.AppName)
}

func (r RemoveRequest) projectName() string {
	return ProjectName(r.AppName)
}

func (r StatusRequest) projectName() string {
	return ProjectName(r.AppName)
}

func (r LogsRequest) projectName() string {
	return ProjectName(r.AppName)
}

func ProjectName(appName string) string {
	return domaincfg.AppIsolationName(appName)
}

func parseServiceStatuses(output string) ([]ServiceStatus, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil, nil
	}

	type statusRow struct {
		Service string `json:"Service"`
		Name    string `json:"Name"`
		State   string `json:"State"`
	}
	var rows []statusRow
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
			return nil, err
		}
	} else {
		decoder := json.NewDecoder(strings.NewReader(trimmed))
		for {
			var row statusRow
			if err := decoder.Decode(&row); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
	}

	statuses := make([]ServiceStatus, 0, len(rows))
	for _, row := range rows {
		name := row.Service
		if name == "" {
			name = row.Name
		}
		statuses = append(statuses, ServiceStatus{Name: name, State: row.State})
	}

	return statuses, nil
}

package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	domaincfg "github.com/sshdock/sshdock/internal/domain"
)

const defaultKeepSuccessfulReleases = 5

type DockerRunner struct {
	executor CommandExecutor
}

type streamingCommandExecutor interface {
	Stream(ctx context.Context, command Command, stdout io.Writer, stderr io.Writer) error
}

func NewDockerRunner(executor CommandExecutor) *DockerRunner {
	return &DockerRunner{executor: executor}
}

func (r *DockerRunner) Deploy(ctx context.Context, request DeployRequest) error {
	projectName := request.projectName()
	baseFiles := []string{request.ComposePath}
	baseArgs := composeArgs(baseFiles, projectName)

	if _, err := ValidateFileWithEnv(request.ComposePath, request.Env); err != nil {
		return NewDeployError(DeployStageValidateCompose, err)
	}
	activeOutput, err := r.executor.Run(ctx, deployCommand(request, commandArgs(baseArgs, "config", "--services")))
	if err != nil {
		return NewDeployError(DeployStageComposeConfig, err)
	}
	if _, err := r.executor.Run(ctx, deployCommand(request, commandArgs(baseArgs, "pull", "--ignore-buildable"))); err != nil {
		return NewDeployError(DeployStagePullImages, err)
	}

	buildServices, err := detectBuildServices(request.ComposePath, serviceSet(activeOutput), request.Env)
	if err != nil {
		return NewDeployError(DeployStageBuildServices, err)
	}

	deployFiles := baseFiles
	if len(buildServices) > 0 {
		overridePath, err := writeReleaseOverride(request.ProjectDir, request.AppName, request.CommitSHA, buildServices)
		if err != nil {
			return NewDeployError(DeployStageBuildServices, err)
		}
		deployFiles = append(deployFiles, overridePath)

		buildArgs := commandArgs(composeArgs(deployFiles, projectName), "build")
		buildArgs = append(buildArgs, buildServices...)
		if _, err := r.executor.Run(ctx, deployCommand(request, buildArgs)); err != nil {
			return NewDeployError(DeployStageBuildServices, err)
		}
	}

	upArgs := commandArgs(composeArgs(deployFiles, projectName), "up", "-d")
	if _, err := r.executor.Run(ctx, deployCommand(request, upArgs)); err != nil {
		return NewDeployError(DeployStageStartContainers, err)
	}

	for _, service := range buildServices {
		if _, err := r.executor.Run(ctx, Command{
			Name: "docker",
			Dir:  request.ProjectDir,
			Args: []string{"image", "tag", releaseImage(request.AppName, service, request.CommitSHA), latestImage(request.AppName, service)},
		}); err != nil {
			return NewDeployError(DeployStageTagImages, err)
		}
	}

	for _, sha := range prunableReleases(request.SuccessfulReleaseSHAs, request.keepReleases()) {
		for _, service := range buildServices {
			image := releaseImage(request.AppName, service, sha)
			_, err := r.executor.Run(ctx, Command{
				Name: "docker",
				Dir:  request.ProjectDir,
				Args: []string{"image", "rm", image},
			})
			if err != nil && request.CleanupRecorder != nil {
				_ = request.CleanupRecorder.RecordCleanupFailure(ctx, CleanupFailure{
					AppName:      request.AppName,
					ServiceName:  service,
					CommitSHA:    sha,
					Image:        image,
					ErrorMessage: err.Error(),
				})
			}
		}
	}

	return nil
}

func (r *DockerRunner) Restart(ctx context.Context, request RestartRequest) error {
	args := commandArgs(composeArgs([]string{request.ComposePath}, request.projectName()), "restart")
	if request.ServiceName != "" {
		args = append(args, request.ServiceName)
	}

	_, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: args})
	return err
}

func (r *DockerRunner) Remove(ctx context.Context, request RemoveRequest) error {
	args := commandArgs(composeArgs([]string{request.ComposePath}, request.projectName()), "down", "--remove-orphans")
	if _, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: args}); err != nil {
		return err
	}

	output, err := r.executor.Run(ctx, Command{
		Name: "docker",
		Dir:  request.ProjectDir,
		Args: []string{"image", "ls", "--format", "{{.Repository}}:{{.Tag}}", "--filter", "reference=sshdock/" + request.AppName + "/*"},
	})
	if err != nil {
		return err
	}
	for _, image := range strings.Split(output, "\n") {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if _, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: []string{"image", "rm", image}}); err != nil {
			return err
		}
	}

	return nil
}

func (r *DockerRunner) Status(ctx context.Context, request StatusRequest) ([]ServiceStatus, error) {
	args := commandArgs(composeArgs([]string{request.ComposePath}, request.projectName()), "ps", "--format", "json")
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

func (r DeployRequest) keepReleases() int {
	if r.KeepReleases > 0 {
		return r.KeepReleases
	}
	return defaultKeepSuccessfulReleases
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

func detectBuildServices(composePath string, activeServices map[string]bool, env map[string]string) ([]string, error) {
	model, err := loadInterpolatedComposeModel(composePath, env)
	if err != nil {
		return nil, err
	}

	services := modelServices(model)
	var buildServices []string
	for serviceName := range services {
		if !activeServices[serviceName] {
			continue
		}
		if serviceHasBuild(serviceName, services, make(map[string]bool)) {
			buildServices = append(buildServices, serviceName)
		}
	}

	sort.Strings(buildServices)
	return buildServices, nil
}

func serviceSet(output string) map[string]bool {
	services := make(map[string]bool)
	for _, name := range strings.Fields(output) {
		services[name] = true
	}
	return services
}

func serviceHasBuild(serviceName string, services map[string]any, visiting map[string]bool) bool {
	if visiting[serviceName] {
		return false
	}
	visiting[serviceName] = true
	defer delete(visiting, serviceName)

	service := modelMapping(services[serviceName])
	if _, found := service["build"]; found {
		return true
	}
	extends := modelMapping(service["extends"])
	baseService, found := modelString(extends["service"])
	if !found {
		return false
	}
	_, found = services[baseService]
	return found && serviceHasBuild(baseService, services, visiting)
}

func writeReleaseOverride(projectDir string, appName string, commitSHA string, services []string) (string, error) {
	overrideDir := filepath.Join(projectDir, ".sshdock")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		return "", err
	}

	overridePath := filepath.Join(overrideDir, "release-"+commitSHA+".compose.yml")
	var builder strings.Builder
	builder.WriteString("services:\n")
	for _, service := range services {
		builder.WriteString("  ")
		builder.WriteString(service)
		builder.WriteString(":\n")
		builder.WriteString("    image: ")
		builder.WriteString(releaseImage(appName, service, commitSHA))
		builder.WriteString("\n")
	}

	if err := os.WriteFile(overridePath, []byte(builder.String()), 0o644); err != nil {
		return "", err
	}

	return overridePath, nil
}

func releaseImage(appName string, serviceName string, tag string) string {
	return "sshdock/" + appName + "/" + serviceName + ":" + tag
}

func latestImage(appName string, serviceName string) string {
	return releaseImage(appName, serviceName, "latest")
}

func prunableReleases(successfulReleaseSHAs []string, keepRecent int) []string {
	priorToKeep := keepRecent - 1
	if priorToKeep < 0 {
		priorToKeep = 0
	}
	if len(successfulReleaseSHAs) <= priorToKeep {
		return nil
	}
	return successfulReleaseSHAs[priorToKeep:]
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

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

	"gopkg.in/yaml.v3"
)

const defaultKeepSuccessfulReleases = 5

type DockerRunner struct {
	executor CommandExecutor
}

func NewDockerRunner(executor CommandExecutor) *DockerRunner {
	return &DockerRunner{executor: executor}
}

func (r *DockerRunner) Validate(ctx context.Context, composePath string) (ValidationResult, error) {
	command := Command{
		Name: "docker",
		Dir:  filepath.Dir(composePath),
		Args: []string{"compose", "-f", composePath, "config"},
	}
	if _, err := r.executor.Run(ctx, command); err != nil {
		return ValidationResult{}, err
	}

	return ValidateFile(composePath)
}

func (r *DockerRunner) Deploy(ctx context.Context, request DeployRequest) error {
	projectName := request.projectName()
	baseFiles := []string{request.ComposePath}
	baseArgs := composeArgs(baseFiles, projectName)

	if _, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: commandArgs(baseArgs, "config")}); err != nil {
		return err
	}
	if _, err := ValidateFile(request.ComposePath); err != nil {
		return err
	}
	if _, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: commandArgs(baseArgs, "pull", "--ignore-buildable")}); err != nil {
		return err
	}

	buildServices, err := detectBuildServices(request.ComposePath)
	if err != nil {
		return err
	}

	deployFiles := baseFiles
	if len(buildServices) > 0 {
		overridePath, err := writeReleaseOverride(request.ProjectDir, request.AppName, request.CommitSHA, buildServices)
		if err != nil {
			return err
		}
		deployFiles = append(deployFiles, overridePath)

		buildArgs := commandArgs(composeArgs(deployFiles, projectName), "build")
		buildArgs = append(buildArgs, buildServices...)
		if _, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: buildArgs}); err != nil {
			return err
		}
	}

	upArgs := commandArgs(composeArgs(deployFiles, projectName), "up", "-d")
	if _, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: upArgs}); err != nil {
		return err
	}

	for _, service := range buildServices {
		if _, err := r.executor.Run(ctx, Command{
			Name: "docker",
			Dir:  request.ProjectDir,
			Args: []string{"image", "tag", releaseImage(request.AppName, service, request.CommitSHA), latestImage(request.AppName, service)},
		}); err != nil {
			return err
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

func (r *DockerRunner) Status(ctx context.Context, request StatusRequest) ([]ServiceStatus, error) {
	args := commandArgs(composeArgs([]string{request.ComposePath}, request.projectName()), "ps", "--format", "json")
	output, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: args})
	if err != nil {
		return nil, err
	}

	return parseServiceStatuses(output)
}

func (r *DockerRunner) Logs(ctx context.Context, request LogsRequest) (string, error) {
	args := commandArgs(composeArgs([]string{request.ComposePath}, request.projectName()), "logs")
	if request.Lines > 0 {
		args = append(args, "--tail", strconv.Itoa(request.Lines))
	}
	if request.ServiceName != "" {
		args = append(args, request.ServiceName)
	}

	return r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: args})
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

func commandArgs(base []string, tail ...string) []string {
	args := append([]string{}, base...)
	return append(args, tail...)
}

func (r DeployRequest) projectName() string {
	if r.ProjectName != "" {
		return r.ProjectName
	}
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

func (r StatusRequest) projectName() string {
	return ProjectName(r.AppName)
}

func (r LogsRequest) projectName() string {
	return ProjectName(r.AppName)
}

func ProjectName(appName string) string {
	name := strings.ToLower(appName)
	var builder strings.Builder
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			builder.WriteRune(char)
			continue
		}
		builder.WriteByte('-')
	}

	normalized := strings.Trim(builder.String(), "-_")
	if normalized == "" {
		normalized = "app"
	}
	return "rhumbase_" + normalized
}

func detectBuildServices(composePath string) ([]string, error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil, err
	}

	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}

	root := documentNode(&document)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("compose file %s must be a mapping", composePath)
	}

	services := mappingValue(root, "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return nil, nil
	}

	var buildServices []string
	for i := 0; i < len(services.Content); i += 2 {
		serviceName := services.Content[i].Value
		service := services.Content[i+1]
		if service.Kind == yaml.MappingNode && mappingValue(service, "build") != nil {
			buildServices = append(buildServices, serviceName)
		}
	}

	sort.Strings(buildServices)
	return buildServices, nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func writeReleaseOverride(projectDir string, appName string, commitSHA string, services []string) (string, error) {
	overrideDir := filepath.Join(projectDir, ".rhumbase")
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
	return "rhumbase/" + appName + "/" + serviceName + ":" + tag
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

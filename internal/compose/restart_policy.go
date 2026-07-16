package compose

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func NonRestartingServices(composePath string, env map[string]string, candidates []string) ([]string, error) {
	model, err := loadInterpolatedComposeModel(composePath, env)
	if err != nil {
		return nil, fmt.Errorf("load Compose restart policies: %w", err)
	}
	return nonRestartingServices(model, candidates), nil
}

func (r *DockerRunner) NonRestartingServices(ctx context.Context, request StatusRequest, candidates []string) ([]string, error) {
	args := commandArgs(composeArgs([]string{request.ComposePath}, request.projectName()), "config", "--format", "json")
	output, err := r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: args, Env: request.Env})
	if err != nil {
		return nil, fmt.Errorf("load effective Compose restart policies: %w", err)
	}
	var model composeModel
	if err := json.Unmarshal([]byte(output), &model); err != nil {
		return nil, fmt.Errorf("decode effective Compose restart policies: %w", err)
	}
	return nonRestartingServices(model, candidates), nil
}

func nonRestartingServices(model composeModel, candidates []string) []string {
	wanted := make(map[string]struct{}, len(candidates))
	for _, name := range candidates {
		wanted[name] = struct{}{}
	}

	modelServiceMap := modelServices(model)
	var services []string
	for _, name := range sortedKeys(modelServiceMap) {
		if _, ok := wanted[name]; !ok {
			continue
		}
		restart, _ := modelString(modelMapping(modelServiceMap[name])["restart"])
		if restart == "" || strings.EqualFold(strings.TrimSpace(restart), "no") {
			services = append(services, name)
		}
	}
	return services
}

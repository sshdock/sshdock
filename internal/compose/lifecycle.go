package compose

import "context"

func (r *DockerRunner) Start(ctx context.Context, request LifecycleRequest) error {
	return r.runLifecycle(ctx, request, "start")
}

func (r *DockerRunner) Stop(ctx context.Context, request LifecycleRequest) error {
	return r.runLifecycle(ctx, request, "stop")
}

func (r *DockerRunner) runLifecycle(ctx context.Context, request LifecycleRequest, operation string) error {
	projectName, err := ProjectName(request.AppName)
	if err != nil {
		return err
	}
	args := commandArgs(composeArgs([]string{request.ComposePath}, projectName), operation)
	_, err = r.executor.Run(ctx, Command{Name: "docker", Dir: request.ProjectDir, Args: args, Env: request.Env})
	return err
}

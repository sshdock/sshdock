package compose

import "context"

type Command struct {
	Name string
	Args []string
	Dir  string
	Env  map[string]string
}

type CommandExecutor interface {
	Run(ctx context.Context, command Command) (string, error)
}

type Runner interface {
	Validate(ctx context.Context, appName string, composePath string) (ValidationResult, error)
	Deploy(ctx context.Context, request DeployRequest) (DeployResult, error)
	Start(ctx context.Context, request LifecycleRequest) error
	Stop(ctx context.Context, request LifecycleRequest) error
	Restart(ctx context.Context, request RestartRequest) error
	Remove(ctx context.Context, request RemoveRequest) error
	Status(ctx context.Context, request StatusRequest) ([]ServiceStatus, error)
	Logs(ctx context.Context, request LogsRequest) (string, error)
}

type DeployRequest struct {
	AppName     string
	ProjectDir  string
	ComposePath string
	ReleaseID   string
	CommitSHA   string
	Env         map[string]string
}

type DeployResult struct {
	RouteTarget RouteTarget
	RouteFound  bool
	RouteReason string
	Warnings    []string
}

type RestartRequest struct {
	AppName     string
	ProjectDir  string
	ComposePath string
	ServiceName string
	Env         map[string]string
}

type LifecycleRequest struct {
	AppName     string
	ProjectDir  string
	ComposePath string
	Env         map[string]string
}

type RemoveRequest struct {
	AppName     string
	ProjectDir  string
	ComposePath string
	Env         map[string]string
}

type StatusRequest struct {
	AppName     string
	ProjectDir  string
	ComposePath string
	Env         map[string]string
}

type LogsRequest struct {
	AppName     string
	ProjectDir  string
	ComposePath string
	ServiceName string
	Lines       int
	Follow      bool
	Env         map[string]string
}

type ServiceStatus struct {
	Name  string
	State string
}

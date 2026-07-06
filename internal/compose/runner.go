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
	Validate(ctx context.Context, composePath string) (ValidationResult, error)
	Deploy(ctx context.Context, request DeployRequest) error
	Restart(ctx context.Context, request RestartRequest) error
	Remove(ctx context.Context, request RemoveRequest) error
	Status(ctx context.Context, request StatusRequest) ([]ServiceStatus, error)
	Logs(ctx context.Context, request LogsRequest) (string, error)
}

type CleanupFailure struct {
	AppName      string
	ServiceName  string
	CommitSHA    string
	Image        string
	ErrorMessage string
}

type CleanupRecorder interface {
	RecordCleanupFailure(ctx context.Context, failure CleanupFailure) error
}

type DeployRequest struct {
	AppName               string
	ProjectDir            string
	ComposePath           string
	ReleaseID             string
	CommitSHA             string
	ProjectName           string
	Env                   map[string]string
	KeepReleases          int
	SuccessfulReleaseSHAs []string
	CleanupRecorder       CleanupRecorder
}

type RestartRequest struct {
	AppName     string
	ProjectDir  string
	ComposePath string
	ServiceName string
}

type RemoveRequest struct {
	AppName     string
	ProjectDir  string
	ComposePath string
}

type StatusRequest struct {
	AppName     string
	ProjectDir  string
	ComposePath string
}

type LogsRequest struct {
	AppName     string
	ProjectDir  string
	ComposePath string
	ServiceName string
	Lines       int
	Follow      bool
}

type ServiceStatus struct {
	Name  string
	State string
}

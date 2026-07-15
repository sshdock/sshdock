package compose

import "context"

type FakeRunner struct {
	Validation   ValidationResult
	DeployResult DeployResult
	Services     []ServiceStatus
	LogOutput    string

	ValidateErr error
	DeployErr   error
	StartErr    error
	StopErr     error
	RestartErr  error
	RemoveErr   error
	StatusErr   error
	LogsErr     error

	ValidatedPath    string
	ValidatedAppName string
	DeployRequests   []DeployRequest
	StartRequests    []LifecycleRequest
	StopRequests     []LifecycleRequest
	RestartRequests  []RestartRequest
	RemoveRequests   []RemoveRequest
	StatusRequests   []StatusRequest
	LogsRequests     []LogsRequest
}

func (f *FakeRunner) Validate(_ context.Context, appName string, composePath string) (ValidationResult, error) {
	f.ValidatedAppName = appName
	f.ValidatedPath = composePath
	if f.ValidateErr != nil {
		return ValidationResult{}, f.ValidateErr
	}

	return f.Validation, nil
}

func (f *FakeRunner) Deploy(_ context.Context, request DeployRequest) (DeployResult, error) {
	f.DeployRequests = append(f.DeployRequests, request)
	return f.DeployResult, f.DeployErr
}

func (f *FakeRunner) Start(_ context.Context, request LifecycleRequest) error {
	f.StartRequests = append(f.StartRequests, request)
	return f.StartErr
}

func (f *FakeRunner) Stop(_ context.Context, request LifecycleRequest) error {
	f.StopRequests = append(f.StopRequests, request)
	return f.StopErr
}

func (f *FakeRunner) Restart(_ context.Context, request RestartRequest) error {
	f.RestartRequests = append(f.RestartRequests, request)
	return f.RestartErr
}

func (f *FakeRunner) Remove(_ context.Context, request RemoveRequest) error {
	f.RemoveRequests = append(f.RemoveRequests, request)
	return f.RemoveErr
}

func (f *FakeRunner) Status(_ context.Context, request StatusRequest) ([]ServiceStatus, error) {
	f.StatusRequests = append(f.StatusRequests, request)
	if f.StatusErr != nil {
		return nil, f.StatusErr
	}

	return f.Services, nil
}

func (f *FakeRunner) Logs(_ context.Context, request LogsRequest) (string, error) {
	f.LogsRequests = append(f.LogsRequests, request)
	if f.LogsErr != nil {
		return "", f.LogsErr
	}

	return f.LogOutput, nil
}

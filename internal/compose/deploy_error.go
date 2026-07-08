package compose

type DeployStage string

const (
	DeployStageComposeConfig   DeployStage = "compose config"
	DeployStageValidateCompose DeployStage = "validate compose"
	DeployStagePullImages      DeployStage = "pull images"
	DeployStageBuildServices   DeployStage = "build services"
	DeployStageStartContainers DeployStage = "start containers"
	DeployStageTagImages       DeployStage = "tag images"
)

type DeployError struct {
	Stage DeployStage
	Err   error
}

func NewDeployError(stage DeployStage, err error) error {
	if err == nil {
		return nil
	}
	return &DeployError{Stage: stage, Err: err}
}

func (e *DeployError) Error() string {
	return string(e.Stage) + " failed: " + e.Err.Error()
}

func (e *DeployError) Unwrap() error {
	return e.Err
}

package deployfailure

import (
	"errors"
	"strings"

	"github.com/sshdock/sshdock/internal/compose"
)

type Error struct {
	message string
	err     error
}

func New(stage string, err error, changed string, fix string, retry string) error {
	if err == nil {
		err = errors.New("unknown deploy failure")
	}
	return Error{message: Message(stage, err.Error(), changed, fix, retry), err: err}
}

func Message(stage string, detail string, changed string, fix string, retry string) string {
	return strings.Join([]string{
		"stage=" + oneLine(stage),
		"detail=" + oneLine(detail),
		"changed=" + oneLine(changed),
		"fix=" + oneLine(fix),
		"retry=" + oneLine(retry),
	}, "; ")
}

func Stage(err error) string {
	var deployErr *compose.DeployError
	if errors.As(err, &deployErr) {
		return string(deployErr.Stage)
	}
	return "deploy"
}

func FixForStage(stage string) string {
	switch compose.DeployStage(stage) {
	case compose.DeployStageComposeConfig, compose.DeployStageValidateCompose:
		return "fix compose.yml; see docs/COMPOSE_SUPPORT.md"
	case compose.DeployStagePullImages:
		return "check image names, registry credentials, and network access"
	case compose.DeployStageBuildServices:
		return "fix Dockerfile or build context errors"
	case compose.DeployStageWaitServices:
		return "inspect docker compose ps and logs; fix services that exited, became unhealthy, or timed out"
	default:
		return "inspect the detail, fix the app or server issue, and retry"
	}
}

func (e Error) Error() string {
	return e.message
}

func (e Error) Unwrap() error {
	return e.err
}

func oneLine(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	parts := strings.Split(value, "\n")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, "; ")
}

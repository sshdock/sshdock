package gitrecv

import (
	"context"
	"fmt"
	"path/filepath"
)

type PushEvent struct {
	AppName   string
	RepoPath  string
	Branch    string
	CommitSHA string
}

type DeployTrigger func(ctx context.Context, event PushEvent) error

type Receiver struct {
	trigger DeployTrigger
}

func NewReceiver(trigger DeployTrigger) *Receiver {
	return &Receiver{trigger: trigger}
}

func BareRepoPath(appsDir string, appName string) string {
	return filepath.Join(appsDir, appName, "repo.git")
}

func (r *Receiver) HandlePush(ctx context.Context, event PushEvent) error {
	if r.trigger == nil {
		return fmt.Errorf("git receiver deploy trigger is not configured")
	}

	return r.trigger(ctx, event)
}

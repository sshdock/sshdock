package gitrecv

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

type pushQueueStore interface {
	QueueDeployment(ctx context.Context, model app.Deployment, expectedCurrentCommit string) error
}

type PushQueueHandlerConfig struct {
	Store           pushQueueStore
	Now             func() time.Time
	NewDeploymentID func() (string, error)
}

type PushQueueHandler struct {
	store           pushQueueStore
	now             func() time.Time
	newDeploymentID func() (string, error)
}

func NewPushQueueHandler(config PushQueueHandlerConfig) *PushQueueHandler {
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	newDeploymentID := config.NewDeploymentID
	if newDeploymentID == nil {
		newDeploymentID = app.NewDeploymentID
	}
	return &PushQueueHandler{store: config.Store, now: now, newDeploymentID: newDeploymentID}
}

func (h *PushQueueHandler) Queue(ctx context.Context, appName string, input io.Reader) (app.Deployment, error) {
	if h.store == nil {
		return app.Deployment{}, fmt.Errorf("push queue store is not configured")
	}
	update, err := readQueuedReceiveUpdate(input)
	if err != nil {
		return app.Deployment{}, err
	}
	id, err := h.newDeploymentID()
	if err != nil {
		return app.Deployment{}, fmt.Errorf("new deployment ID: %w", err)
	}
	deployment := app.Deployment{
		ID:        id,
		AppID:     appName,
		ReleaseID: app.ReleaseID(appName, update.CommitSHA),
		CommitSHA: update.CommitSHA,
		Trigger:   app.DeploymentTriggerPush,
		Status:    app.DeploymentStatusPending,
		StartedAt: h.now(),
	}
	if err := h.store.QueueDeployment(ctx, deployment, update.PreviousCommitSHA); err != nil {
		return app.Deployment{}, fmt.Errorf("queue deployment for app %q: %w", appName, err)
	}
	return deployment, nil
}

type queuedReceiveUpdate struct {
	PreviousCommitSHA string
	CommitSHA         string
}

func readQueuedReceiveUpdate(input io.Reader) (queuedReceiveUpdate, error) {
	scanner := bufio.NewScanner(input)
	var update queuedReceiveUpdate
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			return queuedReceiveUpdate{}, fmt.Errorf("pre-receive line must contain old sha, new sha, and ref")
		}
		if fields[2] != mainRef {
			return queuedReceiveUpdate{}, fmt.Errorf("unsupported destination %q: push to remote main with <source>:main", fields[2])
		}
		if isZeroObjectID(fields[1]) {
			return queuedReceiveUpdate{}, fmt.Errorf("cannot delete remote main: push a commit, branch, or tag to main instead")
		}
		if update.CommitSHA != "" {
			return queuedReceiveUpdate{}, fmt.Errorf("only one remote main update may be queued per push")
		}
		update = queuedReceiveUpdate{PreviousCommitSHA: fields[0], CommitSHA: fields[1]}
	}
	if err := scanner.Err(); err != nil {
		return queuedReceiveUpdate{}, err
	}
	if update.CommitSHA == "" {
		return queuedReceiveUpdate{}, fmt.Errorf("push did not include an update for remote main")
	}
	return update, nil
}

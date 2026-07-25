package gitrecv

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

type postReceiveQueueStore interface {
	FindDeploymentByAppCommit(ctx context.Context, appID string, commitSHA string) (app.Deployment, error)
	RecordDeploymentQueued(ctx context.Context, accepted app.Event, queued app.Event) error
}

type PostReceiveQueueHandlerConfig struct {
	Store             postReceiveQueueStore
	Output            io.Writer
	Now               func() time.Time
	GitUpdateReported bool
}

type PostReceiveQueueHandler struct {
	store             postReceiveQueueStore
	output            io.Writer
	now               func() time.Time
	gitUpdateReported bool
}

func NewPostReceiveQueueHandler(config PostReceiveQueueHandlerConfig) *PostReceiveQueueHandler {
	output := config.Output
	if output == nil {
		output = io.Discard
	}
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &PostReceiveQueueHandler{store: config.Store, output: output, now: now, gitUpdateReported: config.GitUpdateReported}
}

func (h *PostReceiveQueueHandler) Handle(ctx context.Context, appName string, repoPath string, input io.Reader) error {
	if h.store == nil {
		return fmt.Errorf("post-receive queue store is not configured")
	}
	scanner := bufio.NewScanner(input)
	var outputErr error
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		event, err := ParsePostReceiveLine(appName, repoPath, line)
		if err != nil {
			return err
		}
		deployment, err := h.store.FindDeploymentByAppCommit(ctx, event.AppName, event.CommitSHA)
		if err != nil {
			return fmt.Errorf("find queued deployment for current main %s: %w", event.CommitSHA, err)
		}
		recordedAt := h.now()
		if err := h.store.RecordDeploymentQueued(ctx,
			app.Event{ID: EventID(deployment.ID, "git_ref_accepted"), AppID: event.AppName, Type: "git.ref_accepted", Message: "Remote main accepted at " + event.CommitSHA, CreatedAt: recordedAt},
			app.Event{ID: EventID(deployment.ID, "queued"), AppID: event.AppName, Type: "deploy.queued", Message: "Deploy queued for release " + deployment.ReleaseID, CreatedAt: recordedAt},
		); err != nil {
			return fmt.Errorf("record accepted queued deployment %q: %w", deployment.ID, err)
		}
		if !h.gitUpdateReported {
			if _, err := fmt.Fprintf(h.output, "git: remote main updated to %s\n", event.CommitSHA); err != nil {
				outputErr = errors.Join(outputErr, fmt.Errorf("write Git ref status: %w", err))
			}
		}
		if _, err := fmt.Fprintf(h.output, "deploy: queued %s for current main %s\n", deployment.ID, event.CommitSHA); err != nil {
			outputErr = errors.Join(outputErr, fmt.Errorf("write queued deploy status: %w", err))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if outputErr != nil {
		return &StatusOutputError{Err: outputErr}
	}
	return nil
}

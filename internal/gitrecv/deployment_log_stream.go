package gitrecv

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

const defaultDeploymentLogPollInterval = 250 * time.Millisecond

type deploymentLogStreamStore interface {
	DeploymentLog(ctx context.Context, appID string, deploymentID string) (app.DeploymentLog, error)
	ListDeploymentsByApp(ctx context.Context, appID string) ([]app.Deployment, error)
}

type DeploymentLogStreamRequest struct {
	AppID        string
	DeploymentID string
	Follow       bool
}

type DeploymentLogStreamerConfig struct {
	Store  deploymentLogStreamStore
	Poll   func(ctx context.Context) error
	Redact func(string) string
}

type DeploymentLogStreamer struct {
	store  deploymentLogStreamStore
	poll   func(ctx context.Context) error
	redact func(string) string
}

func NewDeploymentLogStreamer(config DeploymentLogStreamerConfig) *DeploymentLogStreamer {
	poll := config.Poll
	if poll == nil {
		poll = func(ctx context.Context) error {
			timer := time.NewTimer(defaultDeploymentLogPollInterval)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	redact := config.Redact
	if redact == nil {
		redact = func(value string) string { return value }
	}
	return &DeploymentLogStreamer{store: config.Store, poll: poll, redact: redact}
}

func (s *DeploymentLogStreamer) Stream(ctx context.Context, request DeploymentLogStreamRequest, output io.Writer) error {
	if s.store == nil {
		return fmt.Errorf("deployment log stream store is not configured")
	}
	if output == nil {
		output = io.Discard
	}
	streamOutput := deploymentLogOutput{writer: output}
	for {
		if err := s.writeLatest(ctx, request, &streamOutput); err != nil {
			return err
		}
		if !request.Follow {
			return nil
		}
		active, err := s.deploymentActive(ctx, request.AppID, request.DeploymentID)
		if err != nil {
			return err
		}
		if !active {
			return s.writeLatest(ctx, request, &streamOutput)
		}
		if err := s.poll(ctx); err != nil {
			return fmt.Errorf("wait for deployment log %q: %w", request.DeploymentID, err)
		}
	}
}

type deploymentLogOutput struct {
	writer  io.Writer
	written int
}

func (s *DeploymentLogStreamer) writeLatest(ctx context.Context, request DeploymentLogStreamRequest, output *deploymentLogOutput) error {
	log, err := s.store.DeploymentLog(ctx, request.AppID, request.DeploymentID)
	if err != nil {
		return fmt.Errorf("load deployment log %q for app %q: %w", request.DeploymentID, request.AppID, err)
	}
	if err := s.write(output.writer, log.Content, &output.written); err != nil {
		return fmt.Errorf("write deployment log %q: %w", request.DeploymentID, err)
	}
	return nil
}

func (s *DeploymentLogStreamer) deploymentActive(ctx context.Context, appID string, deploymentID string) (bool, error) {
	deployments, err := s.store.ListDeploymentsByApp(ctx, appID)
	if err != nil {
		return false, fmt.Errorf("list deployments for app %q: %w", appID, err)
	}
	for _, deployment := range deployments {
		if deployment.ID == deploymentID {
			return deployment.Status == app.DeploymentStatusPending || deployment.Status == app.DeploymentStatusDeploying, nil
		}
	}
	return false, fmt.Errorf("deployment %q not found for app %q", deploymentID, appID)
}

func (s *DeploymentLogStreamer) write(output io.Writer, content string, written *int) error {
	if len(content) <= *written {
		return nil
	}
	if _, err := io.WriteString(output, s.redact(content[*written:])); err != nil {
		return err
	}
	*written = len(content)
	return nil
}

type DeploymentAttachmentError struct {
	AppID        string
	DeploymentID string
	Err          error
}

func (e *DeploymentAttachmentError) Error() string {
	if e.DeploymentID == "" {
		return fmt.Sprintf("deployment log attachment for app %q stopped before deployment selection: %v", e.AppID, e.Err)
	}
	return fmt.Sprintf("deployment %q log attachment stopped: %v", e.DeploymentID, e.Err)
}

func (e *DeploymentAttachmentError) Unwrap() error {
	return e.Err
}

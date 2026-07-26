package gitrecv

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

type deploymentLogSink struct {
	ctx          context.Context
	store        postReceiveStore
	appID        string
	deploymentID string
	now          func() time.Time
}

func (w deploymentLogSink) Write(p []byte) (int, error) {
	if err := w.store.AppendDeploymentLog(w.ctx, w.appID, w.deploymentID, string(p), w.now()); err != nil {
		return 0, fmt.Errorf("append deployment output: %w", err)
	}
	return len(p), nil
}

func (h *PostReceiveHandler) newDeploymentLogWriter(ctx context.Context, appID string, deploymentID string, values map[string]string) *compose.RedactingWriter {
	return compose.NewRedactingWriter(deploymentLogSink{ctx: ctx, store: h.store, appID: appID, deploymentID: deploymentID, now: h.now}, values)
}

func (h *PostReceiveHandler) deployWithOutput(ctx context.Context, request compose.DeployRequest, output io.Writer) (compose.DeployResult, error) {
	if runner, ok := h.runner.(compose.DeploymentOutputRunner); ok {
		return runner.DeployWithOutput(ctx, request, output, output)
	}
	return h.runner.Deploy(ctx, request)
}

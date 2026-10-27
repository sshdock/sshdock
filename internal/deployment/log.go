package deployment

import (
	"context"
	"fmt"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

type deploymentLogSink struct {
	ctx          context.Context
	store        executionStore
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

func (e *Executor) newDeploymentLogWriter(ctx context.Context, deployment app.Deployment, values map[string]string) *compose.RedactingWriter {
	return compose.NewRedactingWriter(deploymentLogSink{ctx: ctx, store: e.store, appID: deployment.AppID, deploymentID: deployment.ID, now: e.now}, values)
}

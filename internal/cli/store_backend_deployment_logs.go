package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/gitrecv"
	"github.com/sshdock/sshdock/internal/store"
)

func (b *StoreBackend) DeploymentLogs(request DeploymentLogRequest, stdout io.Writer) error {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, request.AppName); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("app %q not found", request.AppName)
	} else if err != nil {
		return fmt.Errorf("get app %q: %w", request.AppName, err)
	}
	redactionValues, err := b.configRedactionValues(ctx, request.AppName)
	if err != nil {
		return err
	}
	log, err := b.loadDeploymentLog(ctx, request)
	if err != nil {
		return err
	}
	streamer := gitrecv.NewDeploymentLogStreamer(gitrecv.DeploymentLogStreamerConfig{
		Store: b.store,
		Redact: func(value string) string {
			return compose.RedactValues(value, redactionValues)
		},
	})
	return streamer.Stream(ctx, gitrecv.DeploymentLogStreamRequest{
		AppID:        request.AppName,
		DeploymentID: log.DeploymentID,
		Follow:       request.Follow,
	}, stdout)
}

func (b *StoreBackend) loadDeploymentLog(ctx context.Context, request DeploymentLogRequest) (app.DeploymentLog, error) {
	var (
		log app.DeploymentLog
		err error
	)
	if request.DeploymentID == "" {
		log, err = b.store.LatestDeploymentLog(ctx, request.AppName)
	} else {
		log, err = b.store.DeploymentLog(ctx, request.AppName, request.DeploymentID)
	}
	if errors.Is(err, store.ErrNotFound) {
		if request.DeploymentID == "" {
			return app.DeploymentLog{}, fmt.Errorf("no deployment logs for app %q", request.AppName)
		}
		return app.DeploymentLog{}, fmt.Errorf("deployment %q not found for app %q", request.DeploymentID, request.AppName)
	}
	if err != nil {
		return app.DeploymentLog{}, fmt.Errorf("load deployment log for app %q: %w", request.AppName, err)
	}
	return log, nil
}

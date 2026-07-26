package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

const deploymentLogFollowInterval = 250 * time.Millisecond

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
	written := 0
	if err := writeDeploymentLogOutput(stdout, log.Content, &written, redactionValues); err != nil {
		return err
	}
	if !request.Follow {
		return nil
	}

	ticker := time.NewTicker(deploymentLogFollowInterval)
	defer ticker.Stop()
	for {
		active, err := b.deploymentActive(ctx, request.AppName, log.DeploymentID)
		if err != nil {
			return err
		}
		if !active {
			log, err = b.loadDeploymentLog(ctx, DeploymentLogRequest{AppName: request.AppName, DeploymentID: log.DeploymentID})
			if err != nil {
				return err
			}
			if err := writeDeploymentLogOutput(stdout, log.Content, &written, redactionValues); err != nil {
				return err
			}
			return nil
		}
		<-ticker.C
		log, err = b.loadDeploymentLog(ctx, DeploymentLogRequest{AppName: request.AppName, DeploymentID: log.DeploymentID})
		if err != nil {
			return err
		}
		if err := writeDeploymentLogOutput(stdout, log.Content, &written, redactionValues); err != nil {
			return err
		}
	}
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

func (b *StoreBackend) deploymentActive(ctx context.Context, appName string, deploymentID string) (bool, error) {
	deployments, err := b.store.ListDeploymentsByApp(ctx, appName)
	if err != nil {
		return false, fmt.Errorf("list deployments for app %q: %w", appName, err)
	}
	for _, deployment := range deployments {
		if deployment.ID == deploymentID {
			return deployment.Status == app.DeploymentStatusPending || deployment.Status == app.DeploymentStatusDeploying, nil
		}
	}
	return false, fmt.Errorf("deployment %q not found for app %q", deploymentID, appName)
}

func writeDeploymentLogOutput(stdout io.Writer, content string, written *int, values map[string]string) error {
	if len(content) <= *written {
		return nil
	}
	_, err := io.WriteString(stdout, compose.RedactValues(content[*written:], values))
	if err != nil {
		return err
	}
	*written = len(content)
	return nil
}

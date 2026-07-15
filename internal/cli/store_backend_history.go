package cli

import (
	"context"
	"errors"
	"fmt"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

func (b *StoreBackend) ListReleases(appName string) ([]Release, error) {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, appName); errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("app %q not found", appName)
	} else if err != nil {
		return nil, fmt.Errorf("get app %q: %w", appName, err)
	}
	models, err := b.store.ListReleasesByApp(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list releases for app %q: %w", appName, err)
	}
	deployments, err := b.store.ListDeploymentsByApp(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list deployments for app %q: %w", appName, err)
	}
	redactionValues, err := b.configRedactionValues(ctx, appName)
	if err != nil {
		return nil, err
	}
	failuresByRelease := map[string]string{}
	for _, deployment := range deployments {
		if deployment.Status == appmodel.DeploymentStatusFailed && deployment.ErrorMessage != "" {
			failuresByRelease[deployment.ReleaseID] = compose.RedactValues(deployment.ErrorMessage, redactionValues)
		}
	}
	releases := make([]Release, 0, len(models))
	for _, model := range models {
		releases = append(releases, Release{ID: model.ID, AppName: model.AppID, CommitSHA: model.CommitSHA, ComposePath: model.ComposePath, Status: string(model.Status), Failure: failuresByRelease[model.ID], CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt})
	}
	return releases, nil
}

func (b *StoreBackend) ListDeployments(appName string) ([]Deployment, error) {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, appName); errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("app %q not found", appName)
	} else if err != nil {
		return nil, fmt.Errorf("get app %q: %w", appName, err)
	}
	models, err := b.store.ListDeploymentsByApp(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list deployments for app %q: %w", appName, err)
	}
	redactionValues, err := b.configRedactionValues(ctx, appName)
	if err != nil {
		return nil, err
	}
	deployments := make([]Deployment, 0, len(models))
	for _, model := range models {
		deployments = append(deployments, Deployment{ID: model.ID, AppName: model.AppID, ReleaseID: model.ReleaseID, CommitSHA: model.CommitSHA, Trigger: string(model.Trigger), Status: string(model.Status), StartedAt: model.StartedAt, FinishedAt: model.FinishedAt, FailureStage: model.FailureStage, FailureDetail: compose.RedactValues(model.FailureDetail, redactionValues), RetryGuidance: model.RetryGuidance})
	}
	return deployments, nil
}

func (b *StoreBackend) ListEvents(appName string) ([]Event, error) {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, appName); errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("app %q not found", appName)
	} else if err != nil {
		return nil, fmt.Errorf("get app %q: %w", appName, err)
	}
	models, err := b.store.ListEventsByApp(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list events for app %q: %w", appName, err)
	}
	redactionValues, err := b.configRedactionValues(ctx, appName)
	if err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(models))
	for _, model := range models {
		events = append(events, Event{AppName: model.AppID, Type: model.Type, Message: compose.RedactValues(model.Message, redactionValues), CreatedAt: model.CreatedAt})
	}
	return events, nil
}

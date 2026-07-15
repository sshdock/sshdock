package tui

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

type dashboardConfigRedactor interface {
	RedactionValues(ctx context.Context, appID string) (map[string]string, error)
}

func (h *DashboardHandler) serviceStatusAndLogs(ctx context.Context, model app.App, releases []app.Release) ([]compose.ServiceStatus, map[string]LogsView, error) {
	logsByService := map[string]LogsView{}
	if h.runner == nil {
		return nil, logsByService, nil
	}
	latest, ok := latestRelease(releases)
	if !ok || latest.ComposePath == "" {
		return nil, logsByService, nil
	}
	projectDir := filepath.Dir(latest.ComposePath)
	env, err := h.resolveConfigEnv(ctx, model.ID, projectDir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve config for %s: %w", model.ID, err)
	}
	redactionValues, err := h.redactionEnv(ctx, model, releases)
	if err != nil {
		return nil, nil, err
	}
	services, err := h.runner.Status(ctx, compose.StatusRequest{AppName: model.ID, ProjectDir: projectDir, ComposePath: latest.ComposePath, Env: env})
	if err != nil {
		return nil, nil, compose.RedactError(fmt.Errorf("load service status for %s: %w", model.ID, err), redactionValues)
	}
	for _, service := range services {
		output, err := h.runner.Logs(ctx, compose.LogsRequest{AppName: model.ID, ProjectDir: projectDir, ComposePath: latest.ComposePath, ServiceName: service.Name, Lines: 50, Env: env})
		if err != nil {
			return nil, nil, compose.RedactError(fmt.Errorf("load logs for %s/%s: %w", model.ID, service.Name, err), redactionValues)
		}
		logsByService[service.Name] = NewLogsView(model.ID, service.Name, compose.RedactValues(output, redactionValues))
	}
	return services, logsByService, nil
}

func (h *DashboardHandler) resolveConfigEnv(ctx context.Context, appID string, projectDir string) (map[string]string, error) {
	if h.configResolver == nil {
		return nil, nil
	}
	return h.configResolver.ResolveAppConfig(ctx, appID)
}

func (h *DashboardHandler) redactionEnv(ctx context.Context, model app.App, releases []app.Release) (map[string]string, error) {
	if h.configResolver == nil {
		return nil, nil
	}
	if redactor, ok := h.configResolver.(dashboardConfigRedactor); ok {
		values, err := redactor.RedactionValues(ctx, model.ID)
		if err != nil {
			return nil, fmt.Errorf("load config redaction values for %s: %w", model.ID, err)
		}
		return values, nil
	}
	latest, ok := latestRelease(releases)
	if !ok || latest.ComposePath == "" {
		return nil, nil
	}
	env, err := h.resolveConfigEnv(ctx, model.ID, filepath.Dir(latest.ComposePath))
	if err != nil {
		return nil, fmt.Errorf("resolve config for %s: %w", model.ID, err)
	}
	return env, nil
}

func redactDeployments(deployments []app.Deployment, values map[string]string) []app.Deployment {
	if len(values) == 0 {
		return deployments
	}
	redacted := append([]app.Deployment(nil), deployments...)
	for i := range redacted {
		redacted[i].FailureDetail = compose.RedactValues(redacted[i].FailureDetail, values)
		redacted[i].ErrorMessage = compose.RedactValues(redacted[i].ErrorMessage, values)
	}
	return redacted
}

func redactEvents(events []app.Event, values map[string]string) []app.Event {
	if len(values) == 0 {
		return events
	}
	redacted := append([]app.Event(nil), events...)
	for i := range redacted {
		redacted[i].Message = compose.RedactValues(redacted[i].Message, values)
	}
	return redacted
}

package tui

import (
	"context"
	"fmt"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

type dashboardConfigRedactor interface {
	RedactionValues(ctx context.Context, appID string) (map[string]string, error)
}

func (h *DashboardHandler) Logs(ctx context.Context, appID string, services []ServiceView) (map[string]LogsView, error) {
	logsByService := map[string]LogsView{}
	if h.runner == nil || len(services) == 0 {
		return logsByService, nil
	}
	model, err := h.store.GetApp(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("load app for logs: %w", err)
	}
	projectDir, composePath, err := app.CurrentComposeEntry(model)
	if err != nil {
		return nil, err
	}
	redactionValues, err := h.redactionEnv(ctx, model)
	if err != nil {
		return nil, err
	}
	env, err := app.ResolveRuntimeEnv(ctx, model, h.configResolver)
	if err != nil {
		return nil, compose.RedactError(fmt.Errorf("resolve config for %s: %w", model.ID, err), redactionValues)
	}
	for _, service := range services {
		output, err := h.runner.Logs(ctx, compose.LogsRequest{AppName: model.ID, ProjectDir: projectDir, ComposePath: composePath, ServiceName: service.Name, Lines: 50, Env: env})
		if err != nil {
			output = fmt.Sprintf("Error: load logs for %s/%s: %v", model.ID, service.Name, err)
		}
		logsByService[service.Name] = NewLogsView(model.ID, service.Name, compose.RedactValues(output, redactionValues))
	}
	return logsByService, nil
}

func (h *DashboardHandler) redactionEnv(ctx context.Context, model app.App) (map[string]string, error) {
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
	env, err := h.configResolver.ResolveEnv(ctx, model.ID)
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

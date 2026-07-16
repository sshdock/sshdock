package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sshdock/sshdock/internal/compose"
)

type serviceCommandAction string

const (
	serviceCommandExec serviceCommandAction = "exec"
	serviceCommandRun  serviceCommandAction = "run"
)

func (s *Service) ExecApp(ctx context.Context, request compose.ServiceCommandRequest) error {
	return s.runServiceCommand(ctx, serviceCommandExec, request)
}

func (s *Service) RunApp(ctx context.Context, request compose.ServiceCommandRequest) error {
	return s.runServiceCommand(ctx, serviceCommandRun, request)
}

func (s *Service) runServiceCommand(ctx context.Context, action serviceCommandAction, request compose.ServiceCommandRequest) error {
	if s.serviceCommands == nil {
		return fmt.Errorf("service command runner is not configured")
	}
	if strings.TrimSpace(request.AppName) == "" {
		return fmt.Errorf("app name is required")
	}
	if strings.TrimSpace(request.ServiceName) == "" {
		return fmt.Errorf("service name is required")
	}
	if len(request.Command) == 0 {
		return fmt.Errorf("service command is required")
	}

	target, err := s.currentRuntimeTarget(ctx, request.AppName)
	if err != nil {
		return fmt.Errorf("resolve app %q for %s: %w", request.AppName, action, err)
	}
	now := s.now()
	operationID := restartOperationID(request.AppName, string(action)+"_"+request.ServiceName, now)
	startedType := string(action) + ".started"
	if err := s.store.CreateEvent(ctx, Event{
		ID:        eventID(operationID, startedType),
		AppID:     request.AppName,
		Type:      startedType,
		Message:   serviceCommandMessage(action, "started", request.AppName, request.ServiceName),
		CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("record %s start for app %q: %w", action, request.AppName, err)
	}

	request.ProjectDir = target.projectDir
	request.ComposePath = target.composePath
	request.Env, err = s.resolveDeployEnv(ctx, request.AppName, request.ProjectDir)
	redactionValues := map[string]string(nil)
	if err == nil {
		redactionValues, err = s.resolveRedactionValues(ctx, request.AppName, request.Env)
	}
	if err == nil {
		err = s.executeServiceCommand(ctx, action, request)
	}
	if err != nil {
		failure := compose.RedactError(fmt.Errorf("%s service %q for app %q: %w", action, request.ServiceName, request.AppName, err), redactionValues)
		failedType := string(action) + ".failed"
		eventErr := s.store.CreateEvent(ctx, Event{
			ID:        eventID(operationID, failedType),
			AppID:     request.AppName,
			Type:      failedType,
			Message:   serviceCommandMessage(action, "failed", request.AppName, request.ServiceName),
			CreatedAt: s.now(),
		})
		if eventErr != nil {
			return errors.Join(failure, fmt.Errorf("record %s failure for app %q: %w", action, request.AppName, eventErr))
		}
		return failure
	}

	succeededType := string(action) + ".succeeded"
	if err := s.store.CreateEvent(ctx, Event{
		ID:        eventID(operationID, succeededType),
		AppID:     request.AppName,
		Type:      succeededType,
		Message:   serviceCommandMessage(action, "succeeded", request.AppName, request.ServiceName),
		CreatedAt: s.now(),
	}); err != nil {
		return fmt.Errorf("record %s success for app %q: %w", action, request.AppName, err)
	}
	return nil
}

func (s *Service) executeServiceCommand(ctx context.Context, action serviceCommandAction, request compose.ServiceCommandRequest) error {
	switch action {
	case serviceCommandExec:
		return s.serviceCommands.Exec(ctx, request)
	case serviceCommandRun:
		return s.serviceCommands.RunOneOff(ctx, request)
	default:
		return fmt.Errorf("unsupported service command action %q", action)
	}
}

func serviceCommandMessage(action serviceCommandAction, state string, appName string, serviceName string) string {
	return fmt.Sprintf("%s %s for service %s/%s", strings.ToUpper(string(action[:1]))+string(action[1:]), state, appName, serviceName)
}

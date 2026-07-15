package app

import (
	"context"
	"fmt"

	"github.com/sshdock/sshdock/internal/compose"
)

type restartRequest struct {
	appID         string
	serviceName   string
	startedType   string
	succeededType string
	failedType    string
}

func (s *Service) RestartApp(ctx context.Context, appID string) error {
	return s.restart(ctx, restartRequest{appID: appID, startedType: "restart.started", succeededType: "restart.succeeded", failedType: "restart.failed"})
}

func (s *Service) RestartService(ctx context.Context, appID string, serviceName string) error {
	if serviceName == "" {
		return fmt.Errorf("service name is required")
	}
	return s.restart(ctx, restartRequest{appID: appID, serviceName: serviceName, startedType: "service.restart.started", succeededType: "service.restart.succeeded", failedType: "service.restart.failed"})
}

func (s *Service) restart(ctx context.Context, request restartRequest) error {
	if s.recover == nil {
		return fmt.Errorf("recovery runner is not configured")
	}
	model, release, err := s.latestGoodRelease(ctx, request.appID)
	if err != nil {
		return err
	}
	now := s.now()
	operationID := restartOperationID(request.appID, request.serviceName, now)
	if err := s.store.CreateEvent(ctx, Event{ID: eventID(operationID, request.startedType), AppID: request.appID, Type: request.startedType, Message: restartMessage("Restart started", request.appID, request.serviceName), CreatedAt: now}); err != nil {
		return err
	}

	worktreePath := projectDir(model, release)
	env, err := s.resolveDeployEnv(ctx, request.appID, worktreePath)
	if err != nil {
		err = fmt.Errorf("resolve app config: %w", err)
	} else {
		err = s.recover.Restart(ctx, compose.RestartRequest{AppName: request.appID, ProjectDir: worktreePath, ComposePath: release.ComposePath, ServiceName: request.serviceName, Env: env})
	}
	if err != nil {
		_ = s.store.CreateEvent(ctx, Event{ID: eventID(operationID, request.failedType), AppID: request.appID, Type: request.failedType, Message: restartMessage("Restart failed", request.appID, request.serviceName) + ": " + err.Error(), CreatedAt: s.now()})
		return err
	}

	return s.store.CreateEvent(ctx, Event{ID: eventID(operationID, request.succeededType), AppID: request.appID, Type: request.succeededType, Message: restartMessage("Restart succeeded", request.appID, request.serviceName), CreatedAt: s.now()})
}

func restartMessage(prefix string, appID string, serviceName string) string {
	if serviceName == "" {
		return prefix + " for app " + appID
	}
	return prefix + " for service " + appID + "/" + serviceName
}

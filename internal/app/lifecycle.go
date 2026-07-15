package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/sshdock/sshdock/internal/compose"
)

type lifecycleAction string

const (
	lifecycleStart lifecycleAction = "start"
	lifecycleStop  lifecycleAction = "stop"
)

func (s *Service) StartApp(ctx context.Context, appID string) error {
	return s.runAppLifecycle(ctx, appID, lifecycleStart)
}

func (s *Service) StopApp(ctx context.Context, appID string) error {
	return s.runAppLifecycle(ctx, appID, lifecycleStop)
}

func (s *Service) runAppLifecycle(ctx context.Context, appID string, action lifecycleAction) error {
	if s.recover == nil {
		return fmt.Errorf("lifecycle runner is not configured")
	}
	model, release, err := s.latestGoodRelease(ctx, appID)
	if err != nil {
		return fmt.Errorf("resolve app %q for %s: %w", appID, action, err)
	}

	now := s.now()
	operationID := restartOperationID(appID, string(action), now)
	startedType := string(action) + ".started"
	if err := s.store.CreateEvent(ctx, Event{
		ID:        eventID(operationID, startedType),
		AppID:     appID,
		Type:      startedType,
		Message:   fmt.Sprintf("%s started for app %s", titleLifecycleAction(action), appID),
		CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("record %s start for app %q: %w", action, appID, err)
	}

	worktreePath := projectDir(model, release)
	env, operationErr := s.resolveDeployEnv(ctx, appID, worktreePath)
	if operationErr != nil {
		operationErr = fmt.Errorf("resolve app config: %w", operationErr)
	} else {
		request := compose.LifecycleRequest{AppName: appID, ProjectDir: worktreePath, ComposePath: release.ComposePath, Env: env}
		operationErr = s.executeAppLifecycle(ctx, action, request)
	}
	if operationErr != nil {
		failure := lifecycleFailure(action, appID, operationErr)
		failedType := string(action) + ".failed"
		eventErr := s.store.CreateEvent(ctx, Event{
			ID:        eventID(operationID, failedType),
			AppID:     appID,
			Type:      failedType,
			Message:   failure.Error(),
			CreatedAt: s.now(),
		})
		if eventErr != nil {
			return errors.Join(failure, fmt.Errorf("record %s failure for app %q: %w", action, appID, eventErr))
		}
		return failure
	}

	succeededType := string(action) + ".succeeded"
	if err := s.store.CreateEvent(ctx, Event{
		ID:        eventID(operationID, succeededType),
		AppID:     appID,
		Type:      succeededType,
		Message:   fmt.Sprintf("%s succeeded for app %s", titleLifecycleAction(action), appID),
		CreatedAt: s.now(),
	}); err != nil {
		return fmt.Errorf("record %s success for app %q: %w", action, appID, err)
	}
	return nil
}

func (s *Service) executeAppLifecycle(ctx context.Context, action lifecycleAction, request compose.LifecycleRequest) error {
	switch action {
	case lifecycleStart:
		return s.recover.Start(ctx, request)
	case lifecycleStop:
		return s.recover.Stop(ctx, request)
	default:
		return fmt.Errorf("unsupported lifecycle action %q", action)
	}
}

func lifecycleFailure(action lifecycleAction, appID string, err error) error {
	switch action {
	case lifecycleStart:
		return fmt.Errorf("start app %q: %w; required containers may be missing; run sudo sshdock apps redeploy %s", appID, err, appID)
	case lifecycleStop:
		return fmt.Errorf("stop app %q: %w", appID, err)
	default:
		return fmt.Errorf("%s app %q: %w", action, appID, err)
	}
}

func titleLifecycleAction(action lifecycleAction) string {
	switch action {
	case lifecycleStart:
		return "Start"
	case lifecycleStop:
		return "Stop"
	default:
		return string(action)
	}
}

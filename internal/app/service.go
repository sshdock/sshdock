package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

var ErrNoSuccessfulRelease = errors.New("no successful release")

type serviceStore interface {
	CreateApp(ctx context.Context, model App) error
	GetApp(ctx context.Context, id string) (App, error)
	ListApps(ctx context.Context) ([]App, error)
	UpdateAppStatus(ctx context.Context, id string, status AppStatus, updatedAt time.Time) error
	CreateRelease(ctx context.Context, model Release) error
	GetRelease(ctx context.Context, id string) (Release, error)
	ListReleasesByApp(ctx context.Context, appID string) ([]Release, error)
	UpdateReleaseStatus(ctx context.Context, id string, status ReleaseStatus, updatedAt time.Time) error
	CreateDeployment(ctx context.Context, model Deployment) error
	UpdateDeploymentStatus(ctx context.Context, id string, status DeploymentStatus, finishedAt time.Time, errorMessage string) error
	AttachDomain(ctx context.Context, model Domain) error
	ListDomainsByApp(ctx context.Context, appID string) ([]Domain, error)
	CreateEvent(ctx context.Context, model Event) error
	ListEventsByApp(ctx context.Context, appID string) ([]Event, error)
}

type Service struct {
	store    serviceStore
	now      func() time.Time
	logs     logsRunner
	deploy   deployRunner
	recover  recoveryRunner
	checkout WorktreeCheckout
}

type ServiceOption func(*Service)

type logsRunner interface {
	Logs(ctx context.Context, request compose.LogsRequest) (string, error)
}

type deployRunner interface {
	Deploy(ctx context.Context, request compose.DeployRequest) error
}

type recoveryRunner interface {
	Deploy(ctx context.Context, request compose.DeployRequest) error
	Restart(ctx context.Context, request compose.RestartRequest) error
}

type WorktreeCheckout interface {
	Checkout(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error
}

func NewService(store serviceStore, options ...ServiceOption) *Service {
	service := &Service{
		store: store,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}

	for _, option := range options {
		option(service)
	}

	return service
}

func WithClock(clock func() time.Time) ServiceOption {
	return func(service *Service) {
		service.now = clock
	}
}

func WithLogRunner(runner logsRunner) ServiceOption {
	return func(service *Service) {
		service.logs = runner
	}
}

func WithDeployRunner(runner deployRunner) ServiceOption {
	return func(service *Service) {
		service.deploy = runner
		if recovery, ok := runner.(recoveryRunner); ok {
			service.recover = recovery
		}
	}
}

func WithRecoveryRunner(runner recoveryRunner) ServiceOption {
	return func(service *Service) {
		service.deploy = runner
		service.recover = runner
	}
}

func WithWorktreeCheckout(checkout WorktreeCheckout) ServiceOption {
	return func(service *Service) {
		service.checkout = checkout
	}
}

func (s *Service) CreateApp(ctx context.Context, model App) (App, error) {
	now := s.now()
	if model.Status == "" {
		model.Status = AppStatusCreated
	}
	if model.CreatedAt.IsZero() {
		model.CreatedAt = now
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = now
	}

	if err := s.store.CreateApp(ctx, model); err != nil {
		return App{}, err
	}

	return model, nil
}

func (s *Service) ListApps(ctx context.Context) ([]App, error) {
	return s.store.ListApps(ctx)
}

func (s *Service) CreateRelease(ctx context.Context, model Release) (Release, error) {
	now := s.now()
	if model.Status == "" {
		model.Status = ReleaseStatusPending
	}
	if model.CreatedAt.IsZero() {
		model.CreatedAt = now
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = now
	}

	if err := s.store.CreateRelease(ctx, model); err != nil {
		return Release{}, err
	}

	return model, nil
}

func (s *Service) StartDeployment(ctx context.Context, model Deployment) (Deployment, error) {
	if model.Status == "" {
		model.Status = DeploymentStatusDeploying
	}
	if model.StartedAt.IsZero() {
		model.StartedAt = s.now()
	}

	if err := s.store.CreateDeployment(ctx, model); err != nil {
		return Deployment{}, err
	}

	return model, nil
}

func (s *Service) MarkDeploymentSucceeded(ctx context.Context, deploymentID string) error {
	return s.store.UpdateDeploymentStatus(ctx, deploymentID, DeploymentStatusSucceeded, s.now(), "")
}

func (s *Service) MarkDeploymentFailed(ctx context.Context, deploymentID string, errorMessage string) error {
	return s.store.UpdateDeploymentStatus(ctx, deploymentID, DeploymentStatusFailed, s.now(), errorMessage)
}

func (s *Service) AttachDomain(ctx context.Context, model Domain) (Domain, error) {
	now := s.now()
	if model.CreatedAt.IsZero() {
		model.CreatedAt = now
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = now
	}

	if err := s.store.AttachDomain(ctx, model); err != nil {
		return Domain{}, err
	}

	return model, nil
}

func (s *Service) RollbackRelease(ctx context.Context, appID string, releaseID string, deploymentID string) (Deployment, error) {
	model, err := s.store.GetApp(ctx, appID)
	if err != nil {
		return Deployment{}, err
	}
	release, err := s.store.GetRelease(ctx, releaseID)
	if err != nil {
		return Deployment{}, err
	}
	if release.AppID != appID {
		return Deployment{}, fmt.Errorf("release %q belongs to app %q, not %q", releaseID, release.AppID, appID)
	}

	deployment, err := s.startRecoveryDeployment(ctx, deploymentID, appID, releaseID, "rollback.triggered", "Rollback to release "+releaseID)
	if err != nil {
		return Deployment{}, err
	}

	if s.deploy == nil {
		return deployment, nil
	}

	if err := s.checkoutRelease(ctx, model, release); err != nil {
		_ = s.failRecoveryDeployment(ctx, deployment.ID, appID, "rollback.failed", "Rollback failed for release "+releaseID+": "+err.Error(), err.Error())
		deployment.Status = DeploymentStatusFailed
		deployment.FinishedAt = s.now()
		deployment.ErrorMessage = err.Error()
		return deployment, err
	}
	if err := s.deploy.Deploy(ctx, compose.DeployRequest{
		AppName:     appID,
		ProjectDir:  projectDir(model, release),
		ReleaseID:   release.ID,
		CommitSHA:   release.CommitSHA,
		ComposePath: release.ComposePath,
	}); err != nil {
		_ = s.failRecoveryDeployment(ctx, deployment.ID, appID, "rollback.failed", "Rollback failed for release "+releaseID+": "+err.Error(), err.Error())
		deployment.Status = DeploymentStatusFailed
		deployment.FinishedAt = s.now()
		deployment.ErrorMessage = err.Error()
		return deployment, err
	}

	if err := s.MarkDeploymentSucceeded(ctx, deployment.ID); err != nil {
		return Deployment{}, err
	}
	finishedAt := s.now()
	if err := s.store.UpdateReleaseStatus(ctx, releaseID, ReleaseStatusRolledBack, finishedAt); err != nil {
		return Deployment{}, err
	}
	if err := s.store.UpdateAppStatus(ctx, appID, AppStatusHealthy, finishedAt); err != nil {
		return Deployment{}, err
	}
	if err := s.store.CreateEvent(ctx, Event{
		ID:        eventID(deployment.ID, "rollback_succeeded"),
		AppID:     appID,
		Type:      "rollback.succeeded",
		Message:   "Rollback succeeded for release " + releaseID,
		CreatedAt: finishedAt,
	}); err != nil {
		return Deployment{}, err
	}

	deployment.Status = DeploymentStatusSucceeded
	deployment.FinishedAt = finishedAt
	return deployment, nil
}

func (s *Service) RestartApp(ctx context.Context, appID string) error {
	return s.restart(ctx, appID, "", "restart.triggered", "restart.succeeded", "restart.failed")
}

func (s *Service) RestartService(ctx context.Context, appID string, serviceName string) error {
	if serviceName == "" {
		return fmt.Errorf("service name is required")
	}
	return s.restart(ctx, appID, serviceName, "service.restart.triggered", "service.restart.succeeded", "service.restart.failed")
}

func (s *Service) RedeployLatest(ctx context.Context, appID string, deploymentID string) (Deployment, error) {
	model, release, err := s.latestGoodRelease(ctx, appID)
	if err != nil {
		return Deployment{}, err
	}

	deployment, err := s.startRecoveryDeployment(ctx, deploymentID, appID, release.ID, "redeploy.started", "Redeploy started for release "+release.ID)
	if err != nil {
		return Deployment{}, err
	}
	if s.deploy == nil {
		return deployment, nil
	}

	if err := s.checkoutRelease(ctx, model, release); err != nil {
		_ = s.failRecoveryDeployment(ctx, deployment.ID, appID, "redeploy.failed", "Redeploy failed for release "+release.ID+": "+err.Error(), err.Error())
		deployment.Status = DeploymentStatusFailed
		deployment.FinishedAt = s.now()
		deployment.ErrorMessage = err.Error()
		return deployment, err
	}
	if err := s.deploy.Deploy(ctx, compose.DeployRequest{
		AppName:     appID,
		ProjectDir:  projectDir(model, release),
		ReleaseID:   release.ID,
		CommitSHA:   release.CommitSHA,
		ComposePath: release.ComposePath,
	}); err != nil {
		_ = s.failRecoveryDeployment(ctx, deployment.ID, appID, "redeploy.failed", "Redeploy failed for release "+release.ID+": "+err.Error(), err.Error())
		deployment.Status = DeploymentStatusFailed
		deployment.FinishedAt = s.now()
		deployment.ErrorMessage = err.Error()
		return deployment, err
	}

	finishedAt := s.now()
	if err := s.store.UpdateDeploymentStatus(ctx, deployment.ID, DeploymentStatusSucceeded, finishedAt, ""); err != nil {
		return Deployment{}, err
	}
	if err := s.store.UpdateReleaseStatus(ctx, release.ID, ReleaseStatusSucceeded, finishedAt); err != nil {
		return Deployment{}, err
	}
	if err := s.store.UpdateAppStatus(ctx, appID, AppStatusHealthy, finishedAt); err != nil {
		return Deployment{}, err
	}
	if err := s.store.CreateEvent(ctx, Event{
		ID:        eventID(deployment.ID, "redeploy_succeeded"),
		AppID:     appID,
		Type:      "redeploy.succeeded",
		Message:   "Redeploy succeeded for release " + release.ID,
		CreatedAt: finishedAt,
	}); err != nil {
		return Deployment{}, err
	}

	deployment.Status = DeploymentStatusSucceeded
	deployment.FinishedAt = finishedAt
	return deployment, nil
}

func (s *Service) RecoverDeployedApps(ctx context.Context) error {
	apps, err := s.store.ListApps(ctx)
	if err != nil {
		return err
	}
	for _, model := range apps {
		if _, _, err := s.latestGoodRelease(ctx, model.ID); errors.Is(err, ErrNoSuccessfulRelease) {
			continue
		} else if err != nil {
			return err
		}
		deploymentID := startupRecoveryDeploymentID(model.ID, s.now())
		if _, err := s.RedeployLatest(ctx, model.ID, deploymentID); err != nil {
			return fmt.Errorf("recover app %q: %w", model.ID, err)
		}
	}
	return nil
}

func (s *Service) restart(ctx context.Context, appID string, serviceName string, startedType string, succeededType string, failedType string) error {
	if s.recover == nil {
		return fmt.Errorf("recovery runner is not configured")
	}
	model, release, err := s.latestGoodRelease(ctx, appID)
	if err != nil {
		return err
	}
	now := s.now()
	operationID := restartOperationID(appID, serviceName, now)
	if err := s.store.CreateEvent(ctx, Event{
		ID:        eventID(operationID, startedType),
		AppID:     appID,
		Type:      startedType,
		Message:   restartMessage("Restart started", appID, serviceName),
		CreatedAt: now,
	}); err != nil {
		return err
	}

	err = s.recover.Restart(ctx, compose.RestartRequest{
		AppName:     appID,
		ProjectDir:  projectDir(model, release),
		ComposePath: release.ComposePath,
		ServiceName: serviceName,
	})
	if err != nil {
		_ = s.store.CreateEvent(ctx, Event{
			ID:        eventID(operationID, failedType),
			AppID:     appID,
			Type:      failedType,
			Message:   restartMessage("Restart failed", appID, serviceName) + ": " + err.Error(),
			CreatedAt: s.now(),
		})
		return err
	}

	return s.store.CreateEvent(ctx, Event{
		ID:        eventID(operationID, succeededType),
		AppID:     appID,
		Type:      succeededType,
		Message:   restartMessage("Restart succeeded", appID, serviceName),
		CreatedAt: s.now(),
	})
}

func (s *Service) latestGoodRelease(ctx context.Context, appID string) (App, Release, error) {
	model, err := s.store.GetApp(ctx, appID)
	if err != nil {
		return App{}, Release{}, err
	}
	releases, err := s.store.ListReleasesByApp(ctx, appID)
	if err != nil {
		return App{}, Release{}, err
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].CreatedAt.Equal(releases[j].CreatedAt) {
			return releases[i].ID < releases[j].ID
		}
		return releases[i].CreatedAt.Before(releases[j].CreatedAt)
	})
	for i := len(releases) - 1; i >= 0; i-- {
		if releases[i].Status == ReleaseStatusSucceeded || releases[i].Status == ReleaseStatusRolledBack {
			return model, releases[i], nil
		}
	}
	return App{}, Release{}, fmt.Errorf("%w for app %q", ErrNoSuccessfulRelease, appID)
}

func (s *Service) checkoutRelease(ctx context.Context, model App, release Release) error {
	if s.checkout == nil {
		return nil
	}
	return s.checkout.Checkout(ctx, model.RepoPath, projectDir(model, release), release.CommitSHA)
}

func (s *Service) startRecoveryDeployment(ctx context.Context, deploymentID string, appID string, releaseID string, eventType string, message string) (Deployment, error) {
	now := s.now()
	deployment, err := s.StartDeployment(ctx, Deployment{
		ID:        deploymentID,
		AppID:     appID,
		ReleaseID: releaseID,
		StartedAt: now,
	})
	if err != nil {
		return Deployment{}, err
	}
	if err := s.store.UpdateAppStatus(ctx, appID, AppStatusDeploying, now); err != nil {
		return Deployment{}, err
	}
	if err := s.store.CreateEvent(ctx, Event{
		ID:        eventID(deploymentID, eventType),
		AppID:     appID,
		Type:      eventType,
		Message:   message,
		CreatedAt: now,
	}); err != nil {
		return Deployment{}, err
	}
	return deployment, nil
}

func (s *Service) failRecoveryDeployment(ctx context.Context, deploymentID string, appID string, eventType string, message string, errorMessage string) error {
	finishedAt := s.now()
	_ = s.store.UpdateDeploymentStatus(ctx, deploymentID, DeploymentStatusFailed, finishedAt, errorMessage)
	_ = s.store.UpdateAppStatus(ctx, appID, AppStatusFailed, finishedAt)
	return s.store.CreateEvent(ctx, Event{
		ID:        eventID(deploymentID, eventType),
		AppID:     appID,
		Type:      eventType,
		Message:   message,
		CreatedAt: finishedAt,
	})
}

func projectDir(model App, release Release) string {
	if model.WorktreePath != "" {
		return model.WorktreePath
	}
	return filepath.Dir(release.ComposePath)
}

func eventID(subject string, eventType string) string {
	return "evt_" + sanitizeEventID(subject) + "_" + sanitizeEventID(eventType)
}

func restartOperationID(appID string, serviceName string, now time.Time) string {
	return appID + "_" + serviceName + "_" + now.UTC().Format("20060102150405_000000000")
}

func startupRecoveryDeploymentID(appID string, now time.Time) string {
	return "dep_startup_recover_" + sanitizeEventID(appID) + "_" + now.UTC().Format("20060102150405_000000000")
}

func sanitizeEventID(value string) string {
	result := make([]rune, 0, len(value))
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			result = append(result, char)
		case char >= 'A' && char <= 'Z':
			result = append(result, char)
		case char >= '0' && char <= '9':
			result = append(result, char)
		default:
			result = append(result, '_')
		}
	}
	return string(result)
}

func restartMessage(prefix string, appID string, serviceName string) string {
	if serviceName == "" {
		return prefix + " for app " + appID
	}
	return prefix + " for service " + appID + "/" + serviceName
}

func (s *Service) Logs(ctx context.Context, appName string, serviceName string, lines int) (string, error) {
	if s.logs == nil {
		return "", fmt.Errorf("logs runner is not configured")
	}

	return s.logs.Logs(ctx, compose.LogsRequest{
		AppName:     appName,
		ServiceName: serviceName,
		Lines:       lines,
	})
}

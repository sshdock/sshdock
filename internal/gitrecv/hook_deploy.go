package gitrecv

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/deployfailure"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

type postReceiveStore interface {
	CreateRelease(ctx context.Context, model app.Release) error
	CreateDeployment(ctx context.Context, model app.Deployment) error
	ListReleasesByApp(ctx context.Context, appID string) ([]app.Release, error)
	AttachDomain(ctx context.Context, model app.Domain) error
	ListDomains(ctx context.Context) ([]app.Domain, error)
	GetServerConfig(ctx context.Context) (store.ServerConfig, error)
	UpdateAppStatus(ctx context.Context, id string, status app.AppStatus, updatedAt time.Time) error
	UpdateReleaseStatus(ctx context.Context, id string, status app.ReleaseStatus, updatedAt time.Time) error
	UpdateDeploymentStatus(ctx context.Context, id string, status app.DeploymentStatus, finishedAt time.Time, errorMessage string) error
	CreateEvent(ctx context.Context, model app.Event) error
}

type routeSyncer interface {
	SyncRoutes(ctx context.Context, routes []router.Route) error
}

type configResolver interface {
	ResolveAppConfig(ctx context.Context, appID string, projectDir string) (map[string]string, error)
}

type WorktreeCheckout interface {
	Checkout(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error
}

type WorktreeCheckoutFunc func(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error

func (f WorktreeCheckoutFunc) Checkout(ctx context.Context, repoPath string, worktreePath string, commitSHA string) error {
	return f(ctx, repoPath, worktreePath, commitSHA)
}

type PostReceiveHandlerConfig struct {
	Store          postReceiveStore
	Runner         compose.Runner
	Router         routeSyncer
	ConfigResolver configResolver
	Checkout       WorktreeCheckout
	Now            func() time.Time
}

type PostReceiveHandler struct {
	store          postReceiveStore
	runner         compose.Runner
	router         routeSyncer
	configResolver configResolver
	checkout       WorktreeCheckout
	now            func() time.Time
}

func NewPostReceiveHandler(config PostReceiveHandlerConfig) *PostReceiveHandler {
	now := config.Now
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}

	return &PostReceiveHandler{
		store:          config.Store,
		runner:         config.Runner,
		router:         config.Router,
		configResolver: config.ConfigResolver,
		checkout:       config.Checkout,
		now:            now,
	}
}

func (h *PostReceiveHandler) Handle(ctx context.Context, appName string, repoPath string, worktreePath string, input io.Reader) error {
	if h.store == nil {
		return fmt.Errorf("post-receive store is not configured")
	}
	if h.runner == nil {
		return fmt.Errorf("post-receive compose runner is not configured")
	}
	if h.checkout == nil {
		return fmt.Errorf("post-receive worktree checkout is not configured")
	}
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		event, err := ParsePostReceiveLine(appName, repoPath, line)
		if err != nil {
			return err
		}
		if err := h.handleEvent(ctx, event, worktreePath); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func (h *PostReceiveHandler) handleEvent(ctx context.Context, event PushEvent, worktreePath string) error {
	if err := h.checkout.Checkout(ctx, event.RepoPath, worktreePath, event.CommitSHA); err != nil {
		return err
	}

	composePath, err := compose.DetectFile(worktreePath)
	if err != nil {
		return err
	}
	env, envErr := h.resolveDeployEnv(ctx, event.AppName, worktreePath)
	if envErr == nil {
		_, err = compose.ValidateFileWithEnv(composePath, env)
	}
	if err != nil {
		return err
	}

	now := h.now()
	releaseID := ReleaseID(event.CommitSHA)
	deploymentID := DeploymentID(event.CommitSHA)
	priorSuccessfulReleaseSHAs, err := h.priorSuccessfulReleaseSHAs(ctx, event.AppName)
	if err != nil {
		return err
	}
	if err := h.store.CreateRelease(ctx, app.Release{
		ID:          releaseID,
		AppID:       event.AppName,
		CommitSHA:   event.CommitSHA,
		ComposePath: composePath,
		Status:      app.ReleaseStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		return err
	}
	if err := h.store.UpdateAppStatus(ctx, event.AppName, app.AppStatusDeploying, now); err != nil {
		return err
	}
	if err := h.store.UpdateReleaseStatus(ctx, releaseID, app.ReleaseStatusDeploying, now); err != nil {
		return err
	}
	if err := h.store.CreateDeployment(ctx, app.Deployment{
		ID:        deploymentID,
		AppID:     event.AppName,
		ReleaseID: releaseID,
		Status:    app.DeploymentStatusDeploying,
		StartedAt: now,
	}); err != nil {
		return err
	}
	if err := h.store.CreateEvent(ctx, app.Event{
		ID:        EventID(deploymentID, "started"),
		AppID:     event.AppName,
		Type:      "deploy.started",
		Message:   "Deploy started for release " + releaseID,
		CreatedAt: now,
	}); err != nil {
		return err
	}

	if envErr != nil {
		failure := deployfailure.New(
			"config",
			envErr,
			"release "+releaseID+" and deployment "+deploymentID+" marked failed before Compose started; containers and routes were not changed",
			"set the missing config value(s) with the command(s) in detail",
			"push the same commit again after fixing config",
		)
		finishedAt := h.now()
		_ = h.store.UpdateDeploymentStatus(ctx, deploymentID, app.DeploymentStatusFailed, finishedAt, failure.Error())
		_ = h.store.UpdateReleaseStatus(ctx, releaseID, app.ReleaseStatusFailed, finishedAt)
		_ = h.store.UpdateAppStatus(ctx, event.AppName, app.AppStatusFailed, finishedAt)
		_ = h.store.CreateEvent(ctx, app.Event{
			ID:        EventID(deploymentID, "failed"),
			AppID:     event.AppName,
			Type:      "deploy.failed",
			Message:   "Deploy failed for release " + releaseID + ": " + failure.Error(),
			CreatedAt: finishedAt,
		})
		return failure
	}

	err = h.runner.Deploy(ctx, compose.DeployRequest{
		AppName:               event.AppName,
		ProjectDir:            worktreePath,
		ComposePath:           composePath,
		ReleaseID:             releaseID,
		CommitSHA:             event.CommitSHA,
		Env:                   env,
		KeepReleases:          5,
		SuccessfulReleaseSHAs: priorSuccessfulReleaseSHAs,
		CleanupRecorder: cleanupEventRecorder{
			store:        h.store,
			appID:        event.AppName,
			deploymentID: deploymentID,
			now:          h.now,
		},
	})
	if err != nil {
		err = compose.RedactError(err, env)
		stage := deployfailure.Stage(err)
		failure := deployfailure.New(
			stage,
			err,
			gitPushChangedState(releaseID, deploymentID, stage),
			deployfailure.FixForStage(stage),
			"push the same commit again after fixing the deploy failure",
		)
		finishedAt := h.now()
		_ = h.store.UpdateDeploymentStatus(ctx, deploymentID, app.DeploymentStatusFailed, finishedAt, failure.Error())
		_ = h.store.UpdateReleaseStatus(ctx, releaseID, app.ReleaseStatusFailed, finishedAt)
		_ = h.store.UpdateAppStatus(ctx, event.AppName, app.AppStatusFailed, finishedAt)
		_ = h.store.CreateEvent(ctx, app.Event{
			ID:        EventID(deploymentID, "failed"),
			AppID:     event.AppName,
			Type:      "deploy.failed",
			Message:   "Deploy failed for release " + releaseID + ": " + failure.Error(),
			CreatedAt: finishedAt,
		})
		return failure
	}

	finishedAt := h.now()
	if err := h.store.UpdateDeploymentStatus(ctx, deploymentID, app.DeploymentStatusSucceeded, finishedAt, ""); err != nil {
		return err
	}
	if err := h.store.UpdateReleaseStatus(ctx, releaseID, app.ReleaseStatusSucceeded, finishedAt); err != nil {
		return err
	}
	if err := h.store.UpdateAppStatus(ctx, event.AppName, app.AppStatusHealthy, finishedAt); err != nil {
		return err
	}
	if err := h.store.CreateEvent(ctx, app.Event{
		ID:        EventID(deploymentID, "succeeded"),
		AppID:     event.AppName,
		Type:      "deploy.succeeded",
		Message:   "Deploy succeeded for release " + releaseID,
		CreatedAt: finishedAt,
	}); err != nil {
		return err
	}
	return h.autoRoute(ctx, event.AppName, composePath, deploymentID, finishedAt)
}

func (h *PostReceiveHandler) resolveDeployEnv(ctx context.Context, appName string, projectDir string) (map[string]string, error) {
	if h.configResolver == nil {
		return nil, nil
	}
	return h.configResolver.ResolveAppConfig(ctx, appName, projectDir)
}

func (h *PostReceiveHandler) autoRoute(ctx context.Context, appName string, composePath string, deploymentID string, createdAt time.Time) error {
	config, err := h.store.GetServerConfig(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load server domain config for auto-route: %w", err)
	}
	if config.BaseDomain == "" {
		return nil
	}

	appHost, err := domaincfg.AppHost(appName, config.BaseDomain)
	if err != nil {
		return h.recordAutoRouteSkipped(ctx, appName, deploymentID, err.Error(), createdAt)
	}

	target, ok, reason, err := compose.InferDefaultRoute(composePath)
	if err != nil {
		return h.recordAutoRouteSkipped(ctx, appName, deploymentID, "could not inspect Compose ports: "+err.Error(), createdAt)
	}
	if !ok {
		return h.recordAutoRouteSkipped(ctx, appName, deploymentID, reason, createdAt)
	}

	model := app.Domain{
		ID:          domainID(appName, appHost),
		AppID:       appName,
		ServiceName: target.ServiceName,
		DomainName:  appHost,
		Port:        target.Port,
		HTTPS:       true,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	if err := h.store.AttachDomain(ctx, model); err != nil {
		return h.recordAutoRouteSkipped(ctx, appName, deploymentID, "could not persist auto route "+appHost+": "+err.Error(), createdAt)
	}
	if err := h.store.CreateEvent(ctx, app.Event{
		ID:        EventID(deploymentID, "route_auto_attached"),
		AppID:     appName,
		Type:      "route.auto_attached",
		Message:   "Auto-attached " + appHost + " to " + appName + "/" + target.ServiceName + " on host port " + fmt.Sprint(target.Port),
		CreatedAt: createdAt.Add(time.Second),
	}); err != nil {
		return err
	}

	if h.router == nil {
		return nil
	}
	if err := h.syncRoutesFromStore(ctx); err != nil {
		_ = h.store.CreateEvent(ctx, app.Event{
			ID:    EventID(deploymentID, "router_reload_failed"),
			AppID: appName,
			Type:  "router.reload_failed",
			Message: deployfailure.Message(
				"caddy reload",
				err.Error(),
				"domain was stored, but generated Caddy routes may not be active",
				"run sudo sshdock diagnostics and inspect Caddy",
				"sudo sshdock apps redeploy "+appName,
			),
			CreatedAt: createdAt.Add(2 * time.Second),
		})
		return nil
	}
	return h.store.CreateEvent(ctx, app.Event{
		ID:        EventID(deploymentID, "router_reloaded"),
		AppID:     appName,
		Type:      "router.reloaded",
		Message:   "Reloaded Caddy routes for " + appHost,
		CreatedAt: createdAt.Add(2 * time.Second),
	})
}

func (h *PostReceiveHandler) recordAutoRouteSkipped(ctx context.Context, appName string, deploymentID string, reason string, createdAt time.Time) error {
	message := deployfailure.Message(
		"route inference",
		reason,
		"containers deployed, routes unchanged",
		"attach manually with sshdock domains attach",
		"sudo sshdock domains attach "+appName+" <service> <domain> --port <port>",
	)
	return h.store.CreateEvent(ctx, app.Event{
		ID:        EventID(deploymentID, "route_auto_skipped"),
		AppID:     appName,
		Type:      "route.auto_skipped",
		Message:   message,
		CreatedAt: createdAt.Add(time.Second),
	})
}

func gitPushChangedState(releaseID string, deploymentID string, stage string) string {
	switch compose.DeployStage(stage) {
	case compose.DeployStageComposeConfig, compose.DeployStageValidateCompose, compose.DeployStagePullImages, compose.DeployStageBuildServices:
		return "release " + releaseID + " and deployment " + deploymentID + " marked failed before containers started; routes were not changed"
	case compose.DeployStageStartContainers:
		return "release " + releaseID + " and deployment " + deploymentID + " marked failed while starting containers; routes were not changed"
	default:
		return "release " + releaseID + " and deployment " + deploymentID + " marked failed; inspect the detail before assuming container or route state"
	}
}

func (h *PostReceiveHandler) syncRoutesFromStore(ctx context.Context) error {
	domains, err := h.store.ListDomains(ctx)
	if err != nil {
		return fmt.Errorf("list domains for route rebuild: %w", err)
	}
	return h.router.SyncRoutes(ctx, routesFromDomains(domains))
}

func (h *PostReceiveHandler) priorSuccessfulReleaseSHAs(ctx context.Context, appName string) ([]string, error) {
	releases, err := h.store.ListReleasesByApp(ctx, appName)
	if err != nil {
		return nil, err
	}

	var shas []string
	for i := len(releases) - 1; i >= 0; i-- {
		if releases[i].Status == app.ReleaseStatusSucceeded {
			shas = append(shas, releases[i].CommitSHA)
		}
	}
	return shas, nil
}

func ReleaseID(commitSHA string) string {
	return "rel_" + shortCommitSHA(commitSHA)
}

func DeploymentID(commitSHA string) string {
	return "dep_" + shortCommitSHA(commitSHA)
}

func EventID(deploymentID string, suffix string) string {
	return "evt_" + deploymentID + "_" + suffix
}

func domainID(appName string, domainName string) string {
	return "dom_" + sanitizeIDPart(appName) + "_" + sanitizeIDPart(domainName)
}

func routesFromDomains(domains []app.Domain) []router.Route {
	routes := make([]router.Route, 0, len(domains))
	for _, domain := range domains {
		routes = append(routes, router.Route{
			AppID:       domain.AppID,
			ServiceName: domain.ServiceName,
			DomainName:  domain.DomainName,
			Port:        domain.Port,
			HTTPS:       domain.HTTPS,
		})
	}
	return routes
}

type cleanupEventRecorder struct {
	store        postReceiveStore
	appID        string
	deploymentID string
	now          func() time.Time
}

func (r cleanupEventRecorder) RecordCleanupFailure(ctx context.Context, failure compose.CleanupFailure) error {
	return r.store.CreateEvent(ctx, app.Event{
		ID:        EventID(r.deploymentID+"_"+failure.ServiceName+"_"+failure.CommitSHA, "cleanup_failed"),
		AppID:     r.appID,
		Type:      "cleanup.failed",
		Message:   "Cleanup failed for image " + failure.Image + ": " + failure.ErrorMessage,
		CreatedAt: r.now(),
	})
}

func shortCommitSHA(commitSHA string) string {
	if len(commitSHA) <= 12 {
		return commitSHA
	}
	return commitSHA[:12]
}

func sanitizeIDPart(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}

	return strings.Trim(builder.String(), "_")
}

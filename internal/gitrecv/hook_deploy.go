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
	Output         io.Writer
}

type PostReceiveHandler struct {
	store          postReceiveStore
	runner         compose.Runner
	router         routeSyncer
	configResolver configResolver
	checkout       WorktreeCheckout
	now            func() time.Time
	output         io.Writer
}

func NewPostReceiveHandler(config PostReceiveHandlerConfig) *PostReceiveHandler {
	now := config.Now
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}
	output := config.Output
	if output == nil {
		output = io.Discard
	}

	return &PostReceiveHandler{
		store:          config.Store,
		runner:         config.Runner,
		router:         config.Router,
		configResolver: config.ConfigResolver,
		checkout:       config.Checkout,
		now:            now,
		output:         output,
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

	now := h.now()
	releaseID := ReleaseID(event.CommitSHA)
	deploymentID := DeploymentID(event.CommitSHA)
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
	if _, err := compose.ValidateFileWithEnv(composePath, env); err != nil {
		err = compose.RedactError(err, env)
		stage := string(compose.DeployStageValidateCompose)
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

	result, err := h.runner.Deploy(ctx, compose.DeployRequest{
		AppName:     event.AppName,
		ProjectDir:  worktreePath,
		ComposePath: composePath,
		ReleaseID:   releaseID,
		CommitSHA:   event.CommitSHA,
		Env:         env,
	})
	warningErr := h.recordDeployWarnings(ctx, event.AppName, deploymentID, result.Warnings, env)
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
	succeededMessage := "Deploy succeeded for release " + releaseID
	if warningErr != nil {
		succeededMessage += "; one or more deploy warnings could not be delivered or recorded: " + warningErr.Error()
	}
	if err := h.store.CreateEvent(ctx, app.Event{
		ID:        EventID(deploymentID, "succeeded"),
		AppID:     event.AppName,
		Type:      "deploy.succeeded",
		Message:   succeededMessage,
		CreatedAt: finishedAt,
	}); err != nil {
		return err
	}
	return h.autoRoute(ctx, event.AppName, result, deploymentID, finishedAt)
}

func (h *PostReceiveHandler) resolveDeployEnv(ctx context.Context, appName string, projectDir string) (map[string]string, error) {
	if h.configResolver == nil {
		return nil, nil
	}
	return h.configResolver.ResolveAppConfig(ctx, appName, projectDir)
}

func (h *PostReceiveHandler) autoRoute(ctx context.Context, appName string, result compose.DeployResult, deploymentID string, createdAt time.Time) error {
	domains, err := h.store.ListDomains(ctx)
	if err != nil {
		return fmt.Errorf("list domains before initial auto-route: %w", err)
	}
	for _, domain := range domains {
		if domain.AppID == appName {
			return nil
		}
	}
	if !result.RouteFound {
		return h.recordAutoRouteSkipped(ctx, appName, deploymentID, result.RouteReason, createdAt)
	}
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

	target := result.RouteTarget

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
	if err := h.store.CreateEvent(ctx, app.Event{
		ID:        EventID(deploymentID, "route_auto_skipped"),
		AppID:     appName,
		Type:      "route.auto_skipped",
		Message:   message,
		CreatedAt: createdAt.Add(time.Second),
	}); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(h.output, message); err != nil {
		return nil
	}
	return nil
}

func gitPushChangedState(releaseID string, deploymentID string, stage string) string {
	switch compose.DeployStage(stage) {
	case compose.DeployStageComposeConfig, compose.DeployStageValidateCompose, compose.DeployStagePullImages, compose.DeployStageBuildServices:
		return "release " + releaseID + " and deployment " + deploymentID + " marked failed before containers started; routes were not changed"
	case compose.DeployStageWaitServices:
		return "release " + releaseID + " and deployment " + deploymentID + " marked failed while starting or waiting for services; routes were not changed; no automatic rollback was attempted"
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

func (h *PostReceiveHandler) recordDeployWarnings(ctx context.Context, appName string, deploymentID string, warnings []string, env map[string]string) error {
	var result error
	for index, warning := range warnings {
		warning = compose.RedactValues(warning, env)
		if err := h.store.CreateEvent(ctx, app.Event{
			ID:        EventID(deploymentID, fmt.Sprintf("warning_%d", index+1)),
			AppID:     appName,
			Type:      "deploy.warning",
			Message:   warning,
			CreatedAt: h.now(),
		}); err != nil {
			result = errors.Join(result, fmt.Errorf("record deploy warning: %w", err))
		}
		if _, err := fmt.Fprintln(h.output, "warning: "+warning); err != nil {
			result = errors.Join(result, fmt.Errorf("print deploy warning: %w", err))
		}
	}
	return result
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

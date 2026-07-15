package gitrecv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/deployfailure"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

type autoRouteRequest struct {
	AppName      string
	Result       compose.DeployResult
	DeploymentID string
	CreatedAt    time.Time
}

func (h *PostReceiveHandler) autoRoute(ctx context.Context, request autoRouteRequest) error {
	appName := request.AppName
	result := request.Result
	deploymentID := request.DeploymentID
	createdAt := request.CreatedAt
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
	model := app.Domain{ID: domainID(appName, appHost), AppID: appName, ServiceName: target.ServiceName, DomainName: appHost, Port: target.Port, HTTPS: true, CreatedAt: createdAt, UpdatedAt: createdAt}
	if err := h.store.AttachDomain(ctx, model); err != nil {
		return h.recordAutoRouteSkipped(ctx, appName, deploymentID, "could not persist auto route "+appHost+": "+err.Error(), createdAt)
	}
	if err := h.store.CreateEvent(ctx, app.Event{ID: EventID(deploymentID, "route_auto_attached"), AppID: appName, Type: "route.auto_attached", Message: "Auto-attached " + appHost + " to " + appName + "/" + target.ServiceName + " on host port " + fmt.Sprint(target.Port), CreatedAt: createdAt.Add(time.Second)}); err != nil {
		return err
	}

	if h.router == nil {
		return nil
	}
	if err := h.syncRoutesFromStore(ctx); err != nil {
		failureErr := h.store.UpsertRouteApplyFailure(ctx, store.RouteApplyFailure{
			AppID: appName, ServiceName: model.ServiceName, DomainName: model.DomainName,
			Port: model.Port, HTTPS: model.HTTPS, Operation: store.RouteApplyAttach,
			Detail: err.Error(), UpdatedAt: createdAt.Add(2 * time.Second),
		})
		eventErr := h.store.CreateEvent(ctx, app.Event{ID: EventID(deploymentID, "router_reload_failed"), AppID: appName, Type: "router.reload_failed", Message: deployfailure.Message("caddy reload", err.Error(), "domain was stored, but generated Caddy routes may not be active", "run sudo sshdock diagnostics and inspect Caddy", "sudo sshdock apps redeploy "+appName), CreatedAt: createdAt.Add(2 * time.Second)})
		return errors.Join(failureErr, eventErr)
	}
	if err := h.store.CreateEvent(ctx, app.Event{ID: EventID(deploymentID, "router_reloaded"), AppID: appName, Type: "router.reloaded", Message: "Reloaded Caddy routes for " + appHost, CreatedAt: createdAt.Add(2 * time.Second)}); err != nil {
		return err
	}
	if err := h.store.ClearRouteApplyFailures(ctx); err != nil {
		return fmt.Errorf("Caddy routes reloaded, but clear resolved route failures: %w", err)
	}
	return nil
}

func (h *PostReceiveHandler) recordAutoRouteSkipped(ctx context.Context, appName string, deploymentID string, reason string, createdAt time.Time) error {
	message := deployfailure.Message("route inference", reason, "containers deployed, routes unchanged", "attach manually with sshdock domains attach", "sudo sshdock domains attach "+appName+" <service> <domain> --port <port>")
	if err := h.store.CreateEvent(ctx, app.Event{ID: EventID(deploymentID, "route_auto_skipped"), AppID: appName, Type: "route.auto_skipped", Message: message, CreatedAt: createdAt.Add(time.Second)}); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(h.output, message); err != nil {
		return nil
	}
	return nil
}

func (h *PostReceiveHandler) syncRoutesFromStore(ctx context.Context) error {
	domains, err := h.store.ListDomains(ctx)
	if err != nil {
		return fmt.Errorf("list domains for route rebuild: %w", err)
	}
	if err := h.router.SyncRoutes(ctx, routesFromDomains(domains)); err != nil {
		return err
	}
	return nil
}

func routesFromDomains(domains []app.Domain) []router.Route {
	routes := make([]router.Route, 0, len(domains))
	for _, domain := range domains {
		routes = append(routes, router.Route{AppID: domain.AppID, ServiceName: domain.ServiceName, DomainName: domain.DomainName, Port: domain.Port, HTTPS: domain.HTTPS})
	}
	return routes
}

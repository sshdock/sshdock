package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

func (b *StoreBackend) syncRoutesFromStore(ctx context.Context) error {
	if b.router == nil {
		return nil
	}
	domains, err := b.store.ListDomains(ctx)
	if err != nil {
		return fmt.Errorf("list domains for route rebuild: %w", err)
	}
	if err := b.router.SyncRoutes(ctx, routesFromDomains(domains)); err != nil {
		return err
	}
	return nil
}

func routesFromDomains(domains []appmodel.Domain) []router.Route {
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

func (b *StoreBackend) ListDomains(appName string) ([]Domain, error) {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, appName); errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("app %q not found", appName)
	} else if err != nil {
		return nil, fmt.Errorf("get app %q: %w", appName, err)
	}
	models, err := b.store.ListDomainsByApp(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list domains for app %q: %w", appName, err)
	}

	domains := make([]Domain, 0, len(models))
	for _, model := range models {
		domains = append(domains, cliDomain(model))
	}
	return domains, nil
}

func (b *StoreBackend) AttachDomain(domain Domain) error {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, domain.AppName); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("app %q not found", domain.AppName)
	} else if err != nil {
		return fmt.Errorf("get app %q: %w", domain.AppName, err)
	}
	operationID, err := b.newDeploymentID()
	if err != nil {
		return fmt.Errorf("create domain attach operation ID: %w", err)
	}

	now := b.now()
	model := appmodel.Domain{
		ID:          domainID(domain.AppName, domain.DomainName),
		AppID:       domain.AppName,
		ServiceName: domain.ServiceName,
		DomainName:  domain.DomainName,
		Port:        domain.Port,
		HTTPS:       !strings.HasPrefix(strings.ToLower(domain.DomainName), "http://"),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := b.store.AttachDomain(ctx, model); err != nil {
		return fmt.Errorf("attach domain %q: %w", domain.DomainName, err)
	}
	if err := b.store.CreateEvent(ctx, appmodel.Event{
		ID:        eventID(operationID, "domain_attached"),
		AppID:     model.AppID,
		Type:      "domain.attached",
		Message:   "Attached " + model.DomainName + " to " + model.AppID + "/" + model.ServiceName,
		CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("record domain attach event: %w", err)
	}
	if b.router != nil {
		if err := b.syncRoutesFromStore(ctx); err != nil {
			recordErr := b.recordRouteApplyFailure(ctx, routeApplyFailureRecord{
				failure: store.RouteApplyFailure{
					AppID: model.AppID, ServiceName: model.ServiceName, DomainName: model.DomainName,
					Port: model.Port, HTTPS: model.HTTPS, Operation: store.RouteApplyAttach,
					Detail: err.Error(), UpdatedAt: b.now(),
				},
				event: appmodel.Event{
					ID:        eventID(operationID, "router_reload_failed"),
					AppID:     model.AppID,
					Type:      "router.reload_failed",
					Message:   "Caddy reload failed for " + model.DomainName + ": " + err.Error(),
					CreatedAt: b.now(),
				},
			})
			return errors.Join(fmt.Errorf("reload Caddy routes: %w", err), recordErr)
		}
		if err := b.store.CreateEvent(ctx, appmodel.Event{
			ID:        eventID(operationID, "router_reloaded"),
			AppID:     model.AppID,
			Type:      "router.reloaded",
			Message:   "Reloaded Caddy routes for " + model.DomainName,
			CreatedAt: b.now(),
		}); err != nil {
			return fmt.Errorf("record Caddy reload event: %w", err)
		}
		if err := b.store.ClearRouteApplyFailures(ctx); err != nil {
			return fmt.Errorf("Caddy routes reloaded, but clear resolved route failures: %w", err)
		}
	}

	return nil
}

func (b *StoreBackend) DetachDomain(appName string, domainName string) error {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, appName); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("app %q not found", appName)
	} else if err != nil {
		return fmt.Errorf("get app %q: %w", appName, err)
	}
	operationID, err := b.newDeploymentID()
	if err != nil {
		return fmt.Errorf("create domain detach operation ID: %w", err)
	}
	model, err := b.store.DeleteDomainByAppAndName(ctx, appName, domainName)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("domain %q not found for app %q", domainName, appName)
	}
	if err != nil {
		return fmt.Errorf("detach domain %q: %w", domainName, err)
	}
	if err := b.store.CreateEvent(ctx, appmodel.Event{
		ID:        eventID(operationID, "domain_detached"),
		AppID:     model.AppID,
		Type:      "domain.detached",
		Message:   "Detached " + model.DomainName + " from " + model.AppID,
		CreatedAt: b.now(),
	}); err != nil {
		return fmt.Errorf("record domain detach event: %w", err)
	}
	if b.router != nil {
		if err := b.syncRoutesFromStore(ctx); err != nil {
			recordErr := b.recordRouteApplyFailure(ctx, routeApplyFailureRecord{
				failure: store.RouteApplyFailure{
					AppID: model.AppID, ServiceName: model.ServiceName, DomainName: model.DomainName,
					Port: model.Port, HTTPS: model.HTTPS, Operation: store.RouteApplyDetach,
					Detail: err.Error(), UpdatedAt: b.now(),
				},
				event: appmodel.Event{
					ID:        eventID(operationID, "router_reload_failed_detach"),
					AppID:     model.AppID,
					Type:      "router.reload_failed",
					Message:   "Caddy reload failed after detaching " + model.DomainName + ": " + err.Error(),
					CreatedAt: b.now(),
				},
			})
			return errors.Join(fmt.Errorf("reload Caddy routes: %w", err), recordErr)
		}
		if err := b.store.CreateEvent(ctx, appmodel.Event{
			ID:        eventID(operationID, "router_reloaded_detach"),
			AppID:     model.AppID,
			Type:      "router.reloaded",
			Message:   "Reloaded Caddy routes after detaching " + model.DomainName,
			CreatedAt: b.now(),
		}); err != nil {
			return fmt.Errorf("record Caddy reload event: %w", err)
		}
		if err := b.store.ClearRouteApplyFailures(ctx); err != nil {
			return fmt.Errorf("Caddy routes reloaded, but clear resolved route failures: %w", err)
		}
	}

	return nil
}

type routeApplyFailureRecord struct {
	failure store.RouteApplyFailure
	event   appmodel.Event
}

func (b *StoreBackend) recordRouteApplyFailure(ctx context.Context, record routeApplyFailureRecord) error {
	failureErr := b.store.UpsertRouteApplyFailure(ctx, record.failure)
	eventErr := b.store.CreateEvent(ctx, record.event)
	return errors.Join(failureErr, eventErr)
}

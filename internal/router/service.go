package router

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/deploycoord"
	"github.com/sshdock/sshdock/internal/deployfailure"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/store"
)

type routingStore interface {
	GetApp(context.Context, string) (app.App, error)
	AttachDomain(context.Context, app.Domain) error
	DeleteDomainByAppAndName(context.Context, string, string) (app.Domain, error)
	ListDomains(context.Context) ([]app.Domain, error)
	CreateEvent(context.Context, app.Event) error
	UpsertRouteApplyFailure(context.Context, store.RouteApplyFailure) error
	ClearRouteApplyFailures(context.Context) error
	GetServerConfig(context.Context) (store.ServerConfig, error)
}

type routeApplier interface {
	SyncRoutes(context.Context, []Route) error
}

type ServiceConfig struct {
	Store          routingStore
	Router         routeApplier
	LocksDir       string
	Now            func() time.Time
	NewOperationID func() (string, error)
}

type Service struct {
	store    routingStore
	router   routeApplier
	locksDir string
	now      func() time.Time
	newID    func() (string, error)
}

type AttachRequest struct {
	AppID       string
	ServiceName string
	DomainName  string
	Port        int
}

type InitialRouteRequest struct {
	AppID          string
	DeploymentID   string
	Target         *Route
	NoTargetReason string
}

func NewService(config ServiceConfig) *Service {
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.NewOperationID == nil {
		config.NewOperationID = app.NewDeploymentID
	}
	return &Service{store: config.Store, router: config.Router, locksDir: config.LocksDir, now: config.Now, newID: config.NewOperationID}
}

func (s *Service) Attach(ctx context.Context, request AttachRequest) (resultErr error) {
	address, https, err := canonicalAddress(request.DomainName)
	if err != nil {
		return err
	}
	if err := validateTarget(request.ServiceName, request.Port); err != nil {
		return err
	}
	guard, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, guard.Release()) }()
	if _, err := s.store.GetApp(ctx, request.AppID); err != nil {
		return fmt.Errorf("get app %q: %w", request.AppID, err)
	}
	domains, err := s.store.ListDomains(ctx)
	if err != nil {
		return fmt.Errorf("list domains before attach: %w", err)
	}
	id, err := availableDomainID(request.AppID, address, domains)
	if err != nil {
		return err
	}
	operationID, err := s.newID()
	if err != nil {
		return fmt.Errorf("create domain attach operation ID: %w", err)
	}
	now := s.now()
	model := app.Domain{ID: id, AppID: request.AppID, ServiceName: request.ServiceName, DomainName: address,
		Port: request.Port, HTTPS: https, CreatedAt: now, UpdatedAt: now}
	if err := s.store.AttachDomain(ctx, model); err != nil {
		return fmt.Errorf("attach domain %q: %w", address, err)
	}
	auditErr := s.store.CreateEvent(context.WithoutCancel(ctx), app.Event{ID: routeEventID(operationID, "domain_attached"), AppID: model.AppID,
		Type: "domain.attached", Message: "Attached " + address + " to " + model.AppID + "/" + model.ServiceName, CreatedAt: now})
	if auditErr != nil {
		auditErr = fmt.Errorf("record domain attach event: %w", auditErr)
	}
	return errors.Join(auditErr, s.apply(ctx, model, store.RouteApplyAttach, operationID, false))
}

func (s *Service) Detach(ctx context.Context, appID, domainName string) (resultErr error) {
	address, _, err := canonicalAddress(domainName)
	if err != nil {
		return err
	}
	guard, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, guard.Release()) }()
	if _, err := s.store.GetApp(ctx, appID); err != nil {
		return fmt.Errorf("get app %q: %w", appID, err)
	}
	domains, err := s.store.ListDomains(ctx)
	if err != nil {
		return fmt.Errorf("list domains before detach: %w", err)
	}
	key, err := publicAddressKey(address)
	if err != nil {
		return err
	}
	var matches []app.Domain
	for _, domain := range domains {
		storedKey, err := publicAddressKey(domain.DomainName)
		if err == nil && domain.AppID == appID && storedKey == key {
			matches = append(matches, domain)
		}
	}
	if len(matches) == 0 {
		return fmt.Errorf("domain %q not found for app %q: %w", domainName, appID, store.ErrNotFound)
	}
	operationID, err := s.newID()
	if err != nil {
		return fmt.Errorf("create domain detach operation ID: %w", err)
	}
	for _, domain := range matches {
		if _, err := s.store.DeleteDomainByAppAndName(ctx, appID, domain.DomainName); err != nil {
			return fmt.Errorf("detach domain %q: %w", domain.DomainName, err)
		}
	}
	model := matches[0]
	model.DomainName = address
	auditErr := s.store.CreateEvent(context.WithoutCancel(ctx), app.Event{ID: routeEventID(operationID, "domain_detached"), AppID: appID,
		Type: "domain.detached", Message: "Detached " + address + " from " + appID, CreatedAt: s.now()})
	if auditErr != nil {
		auditErr = fmt.Errorf("record domain detach event: %w", auditErr)
	}
	return errors.Join(auditErr, s.apply(ctx, model, store.RouteApplyDetach, operationID, false))
}

// EnsureInitial returns guidance only when the app still has no route.
func (s *Service) EnsureInitial(ctx context.Context, request InitialRouteRequest) (skipReason string, resultErr error) {
	guard, err := s.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, guard.Release()) }()
	if _, err := s.store.GetApp(ctx, request.AppID); err != nil {
		return "", fmt.Errorf("get app %q: %w", request.AppID, err)
	}
	if request.DeploymentID == "" {
		return "", fmt.Errorf("initial route requires a deployment ID")
	}
	domains, err := s.store.ListDomains(ctx)
	if err != nil {
		return "", fmt.Errorf("list domains before initial auto-route: %w", err)
	}
	for _, domain := range domains {
		if domain.AppID == request.AppID {
			return "", nil
		}
	}
	if request.Target == nil {
		if request.NoTargetReason == "" {
			request.NoTargetReason = "no safe host-published Compose port was found"
		}
		return request.NoTargetReason, nil
	}
	if err := validateTarget(request.Target.ServiceName, request.Target.Port); err != nil {
		return err.Error(), nil
	}
	config, err := s.store.GetServerConfig(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load server domain config for auto-route: %w", err)
	}
	if config.BaseDomain == "" {
		return "", nil
	}
	host, err := domaincfg.AppHost(request.AppID, config.BaseDomain)
	if err != nil {
		return err.Error(), nil
	}
	address, https, err := canonicalAddress(host)
	if err != nil {
		return err.Error(), nil
	}
	id, err := availableDomainID(request.AppID, address, domains)
	if err != nil {
		return err.Error(), nil
	}
	now := s.now()
	model := app.Domain{ID: id, AppID: request.AppID, ServiceName: request.Target.ServiceName, DomainName: address,
		Port: request.Target.Port, HTTPS: https, CreatedAt: now, UpdatedAt: now}
	if err := s.store.AttachDomain(ctx, model); err != nil {
		return "could not persist auto route " + address + ": " + err.Error(), nil
	}
	auditErr := s.store.CreateEvent(context.WithoutCancel(ctx), app.Event{ID: routeEventID(request.DeploymentID, "route_auto_attached"), AppID: model.AppID,
		Type: "route.auto_attached", Message: "Auto-attached " + address + " to " + model.AppID + "/" + model.ServiceName + " on host port " + fmt.Sprint(model.Port), CreatedAt: now.Add(time.Second)})
	return "", errors.Join(auditErr, s.apply(ctx, model, store.RouteApplyAttach, request.DeploymentID, true))
}

// Reconcile applies current intent even after the app that prompted it has been removed.
func (s *Service) Reconcile(ctx context.Context) (resultErr error) {
	guard, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, guard.Release()) }()
	if err := s.rebuild(ctx); err != nil {
		return fmt.Errorf("reload Caddy routes: %w", err)
	}
	return s.clearFailures(context.WithoutCancel(ctx))
}

func (s *Service) acquire(ctx context.Context) (*deploycoord.Guard, error) {
	if s.store == nil || s.router == nil || s.locksDir == "" {
		return nil, fmt.Errorf("routing store, Caddy router, and locks directory must be configured")
	}
	return deploycoord.NewManager(s.locksDir).AcquireRoutes(ctx)
}

func (s *Service) rebuild(ctx context.Context) error {
	domains, err := s.store.ListDomains(ctx)
	if err != nil {
		return fmt.Errorf("list domains for route rebuild: %w", err)
	}
	routes := make([]Route, 0, len(domains))
	owners := make(map[string]string, len(domains))
	for _, domain := range domains {
		address, https, err := canonicalAddress(domain.DomainName)
		if err != nil {
			return err
		}
		if err := validateTarget(domain.ServiceName, domain.Port); err != nil {
			return fmt.Errorf("stored route %q: %w", domain.DomainName, err)
		}
		key, err := publicAddressKey(address)
		if err != nil {
			return err
		}
		if owner, exists := owners[key]; exists {
			return fmt.Errorf("public address %q has duplicate routing intent for apps %q and %q; detach the conflicting address and retry", address, owner, domain.AppID)
		}
		owners[key] = domain.AppID
		routes = append(routes, Route{AppID: domain.AppID, ServiceName: domain.ServiceName, DomainName: address, Port: domain.Port, HTTPS: https})
	}
	return s.router.SyncRoutes(ctx, routes)
}

func (s *Service) apply(ctx context.Context, model app.Domain, operation store.RouteApplyOperation, operationID string, automatic bool) error {
	applyErr := s.rebuild(ctx)
	terminalCtx := context.WithoutCancel(ctx)
	now := s.now()
	if automatic {
		now = now.Add(2 * time.Second)
	}
	if applyErr != nil {
		failureErr := s.store.UpsertRouteApplyFailure(terminalCtx, store.RouteApplyFailure{AppID: model.AppID, ServiceName: model.ServiceName,
			DomainName: model.DomainName, Port: model.Port, HTTPS: model.HTTPS, Operation: operation, Detail: applyErr.Error(), UpdatedAt: now})
		message := "Caddy reload failed for " + model.DomainName + ": " + applyErr.Error()
		if operation == store.RouteApplyDetach {
			message = "Caddy reload failed after detaching " + model.DomainName + ": " + applyErr.Error()
		}
		if automatic {
			retry := fmt.Sprintf("sudo sshdock domains attach %s %s %s --port %d", model.AppID, model.ServiceName, model.DomainName, model.Port)
			message = deployfailure.Message("caddy reload", applyErr.Error(), "domain was stored, but generated Caddy routes may not be active", "run sudo sshdock diagnostics and inspect Caddy", retry)
		}
		eventErr := s.store.CreateEvent(terminalCtx, app.Event{ID: routeEventID(operationID, "router_reload_failed"), AppID: model.AppID,
			Type: "router.reload_failed", Message: message, CreatedAt: now})
		if automatic {
			return errors.Join(failureErr, eventErr)
		}
		return errors.Join(fmt.Errorf("reload Caddy routes: %w", applyErr), failureErr, eventErr)
	}
	message := "Reloaded Caddy routes for " + model.DomainName
	if operation == store.RouteApplyDetach {
		message = "Reloaded Caddy routes after detaching " + model.DomainName
	}
	eventErr := s.store.CreateEvent(terminalCtx, app.Event{ID: routeEventID(operationID, "router_reloaded"), AppID: model.AppID,
		Type: "router.reloaded", Message: message, CreatedAt: now})
	if eventErr != nil {
		eventErr = fmt.Errorf("record Caddy reload event: %w", eventErr)
	}
	return errors.Join(eventErr, s.clearFailures(terminalCtx))
}

func (s *Service) clearFailures(ctx context.Context) error {
	if err := s.store.ClearRouteApplyFailures(ctx); err != nil {
		return fmt.Errorf("Caddy routes reloaded, but clear resolved route failures: %w", err)
	}
	return nil
}

func availableDomainID(appID, address string, domains []app.Domain) (string, error) {
	key, err := publicAddressKey(address)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(appID + "\x00" + key))
	id := fmt.Sprintf("dom_%x", sum)
	matched := false
	for _, domain := range domains {
		storedKey, err := publicAddressKey(domain.DomainName)
		if err != nil || storedKey != key {
			continue
		}
		if domain.AppID != appID {
			return "", fmt.Errorf("public address %q is already attached to app %q; detach it there before attaching it to %q", address, domain.AppID, appID)
		}
		if matched {
			return "", fmt.Errorf("public address %q has duplicate stored entries; detach it and retry", address)
		}
		matched, id = true, domain.ID
	}
	return id, nil
}

func routeEventID(operationID, suffix string) string {
	return "evt_" + operationID + "_" + suffix
}

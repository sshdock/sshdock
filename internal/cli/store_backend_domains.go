package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

func (b *StoreBackend) ListDomains(appName string) ([]Domain, error) {
	ctx := context.Background()
	if _, err := b.store.GetApp(ctx, appName); errors.Is(err, store.ErrNotFound) {
		return nil, appNotFoundError(appName)
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

func (b *StoreBackend) AttachDomain(model Domain) error {
	return b.routingService().Attach(context.Background(), router.AttachRequest{
		AppID: model.AppName, ServiceName: model.ServiceName, DomainName: model.DomainName, Port: model.Port,
	})
}

func (b *StoreBackend) DetachDomain(appName, domainName string) error {
	return b.routingService().Detach(context.Background(), appName, domainName)
}

func (b *StoreBackend) routingService() *router.Service {
	return router.NewService(router.ServiceConfig{Store: b.store, Router: b.router, LocksDir: b.locksDir, Now: b.now, NewOperationID: b.newDeploymentID})
}

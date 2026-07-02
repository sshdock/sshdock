package router

import (
	"context"
	"sort"
)

type FakeRouter struct {
	AttachErr error
	DetachErr error
	ReloadErr error
	RoutesErr error

	ReloadCount int

	routes map[string]Route
}

func NewFakeRouter() *FakeRouter {
	return &FakeRouter{routes: map[string]Route{}}
}

func (f *FakeRouter) AttachDomain(_ context.Context, route Route) error {
	if f.AttachErr != nil {
		return f.AttachErr
	}

	f.routes[route.DomainName] = route
	return nil
}

func (f *FakeRouter) DetachDomain(_ context.Context, domainName string) error {
	if f.DetachErr != nil {
		return f.DetachErr
	}

	delete(f.routes, domainName)
	return nil
}

func (f *FakeRouter) SyncRoutes(_ context.Context, routes []Route) error {
	if f.AttachErr != nil {
		return f.AttachErr
	}

	f.routes = map[string]Route{}
	for _, route := range routes {
		f.routes[route.DomainName] = route
	}
	return nil
}

func (f *FakeRouter) Reload(_ context.Context) error {
	if f.ReloadErr != nil {
		return f.ReloadErr
	}

	f.ReloadCount++
	return nil
}

func (f *FakeRouter) Routes(_ context.Context) ([]Route, error) {
	if f.RoutesErr != nil {
		return nil, f.RoutesErr
	}

	domains := make([]string, 0, len(f.routes))
	for domain := range f.routes {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	routes := make([]Route, 0, len(domains))
	for _, domain := range domains {
		routes = append(routes, f.routes[domain])
	}

	return routes, nil
}

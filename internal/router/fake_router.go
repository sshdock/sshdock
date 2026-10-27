package router

import (
	"context"
	"sort"
)

type FakeRouter struct {
	SyncErr   error
	RoutesErr error

	routes map[string]Route
}

func NewFakeRouter() *FakeRouter {
	return &FakeRouter{routes: map[string]Route{}}
}

func (f *FakeRouter) SyncRoutes(_ context.Context, routes []Route) error {
	if f.SyncErr != nil {
		return f.SyncErr
	}

	f.routes = map[string]Route{}
	for _, route := range routes {
		f.routes[route.DomainName] = route
	}
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

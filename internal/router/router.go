package router

import "context"

type Route struct {
	AppID       string
	ServiceName string
	DomainName  string
	Port        int
	HTTPS       bool
}

type Router interface {
	AttachDomain(ctx context.Context, route Route) error
	DetachDomain(ctx context.Context, domainName string) error
	SyncRoutes(ctx context.Context, routes []Route) error
	Reload(ctx context.Context) error
	Routes(ctx context.Context) ([]Route, error)
}

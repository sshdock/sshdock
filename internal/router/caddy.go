package router

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CaddyCommand struct {
	Name string
	Args []string
	Dir  string
}

type CaddyCommandExecutor interface {
	Run(ctx context.Context, command CaddyCommand) error
}

type CaddyRouterConfig struct {
	ConfigPath string
	Executor   CaddyCommandExecutor
}

type CaddyRouter struct {
	configPath string
	executor   CaddyCommandExecutor
	routes     map[string]Route
}

func NewCaddyRouter(config CaddyRouterConfig) *CaddyRouter {
	return &CaddyRouter{
		configPath: config.ConfigPath,
		executor:   config.Executor,
		routes:     map[string]Route{},
	}
}

func (r *CaddyRouter) AttachDomain(ctx context.Context, route Route) error {
	r.routes[route.DomainName] = route
	if err := r.writeConfig(); err != nil {
		return err
	}

	return r.Reload(ctx)
}

func (r *CaddyRouter) DetachDomain(ctx context.Context, domainName string) error {
	delete(r.routes, domainName)
	if err := r.writeConfig(); err != nil {
		return err
	}

	return r.Reload(ctx)
}

func (r *CaddyRouter) Reload(ctx context.Context) error {
	if r.executor == nil {
		return nil
	}

	return r.executor.Run(ctx, CaddyCommand{Name: "caddy", Args: []string{"reload", "--config", r.configPath}})
}

func (r *CaddyRouter) Routes(_ context.Context) ([]Route, error) {
	domains := make([]string, 0, len(r.routes))
	for domain := range r.routes {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	routes := make([]Route, 0, len(domains))
	for _, domain := range domains {
		routes = append(routes, r.routes[domain])
	}

	return routes, nil
}

func (r *CaddyRouter) writeConfig() error {
	if err := os.MkdirAll(filepath.Dir(r.configPath), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(r.configPath), ".rhumbase-caddy-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.WriteString(renderCaddyfile(r.routes)); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, r.configPath)
}

func renderCaddyfile(routes map[string]Route) string {
	domains := make([]string, 0, len(routes))
	for domain := range routes {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	var builder strings.Builder
	for _, domain := range domains {
		route := routes[domain]
		builder.WriteString(domain)
		builder.WriteString(" {\n")
		builder.WriteString("\treverse_proxy ")
		builder.WriteString(route.ServiceName)
		builder.WriteString(":")
		builder.WriteString(portString(route.Port))
		builder.WriteString("\n")
		builder.WriteString("}\n\n")
	}

	return builder.String()
}

func portString(port int) string {
	if port == 0 {
		return "80"
	}

	digits := []byte{}
	for port > 0 {
		digits = append([]byte{byte('0' + port%10)}, digits...)
		port /= 10
	}

	return string(digits)
}

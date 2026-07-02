package router

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	ConfigPath   string
	Executor     CaddyCommandExecutor
	UpstreamHost string
	AdminAddress string
}

type CaddyRouter struct {
	configPath   string
	executor     CaddyCommandExecutor
	upstreamHost string
	adminAddress string
	routes       map[string]Route
}

func NewCaddyRouter(config CaddyRouterConfig) *CaddyRouter {
	if config.UpstreamHost == "" {
		config.UpstreamHost = "127.0.0.1"
	}

	return &CaddyRouter{
		configPath:   config.ConfigPath,
		executor:     config.Executor,
		upstreamHost: config.UpstreamHost,
		adminAddress: config.AdminAddress,
		routes:       map[string]Route{},
	}
}

func (r *CaddyRouter) AttachDomain(ctx context.Context, route Route) error {
	routes := copyRoutes(r.routes)
	routes[route.DomainName] = route

	return r.SyncRoutes(ctx, routesFromMap(routes))
}

func (r *CaddyRouter) DetachDomain(ctx context.Context, domainName string) error {
	routes := copyRoutes(r.routes)
	delete(routes, domainName)

	return r.SyncRoutes(ctx, routesFromMap(routes))
}

func (r *CaddyRouter) SyncRoutes(ctx context.Context, routes []Route) error {
	routeMap := routesByDomain(routes)
	if err := r.writeConfig(ctx, routeMap); err != nil {
		return err
	}

	r.routes = routeMap
	return r.Reload(ctx)
}

func (r *CaddyRouter) Reload(ctx context.Context) error {
	if r.executor == nil {
		return nil
	}

	args := []string{"reload", "--config", r.configPath}
	if r.adminAddress != "" {
		args = append(args, "--address", r.adminAddress)
	}
	return r.executor.Run(ctx, CaddyCommand{Name: "caddy", Args: args})
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

func (r *CaddyRouter) writeConfig(ctx context.Context, routes map[string]Route) error {
	if err := os.MkdirAll(filepath.Dir(r.configPath), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(r.configPath), ".rhumbase-caddy-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.WriteString(renderCaddyfile(routes, r.upstreamHost, r.adminAddress)); err != nil {
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
	if err := r.validateConfig(ctx, tmpPath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, r.configPath)
}

func (r *CaddyRouter) validateConfig(ctx context.Context, configPath string) error {
	if r.executor == nil {
		return nil
	}

	return r.executor.Run(ctx, CaddyCommand{Name: "caddy", Args: []string{"validate", "--config", configPath}})
}

func renderCaddyfile(routes map[string]Route, upstreamHost string, adminAddress string) string {
	domains := make([]string, 0, len(routes))
	for domain := range routes {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	var builder strings.Builder
	if adminAddress != "" {
		builder.WriteString("{\n")
		builder.WriteString("\tadmin ")
		builder.WriteString(adminAddress)
		builder.WriteString("\n")
		builder.WriteString("}\n\n")
	}
	for _, domain := range domains {
		route := routes[domain]
		builder.WriteString(domain)
		builder.WriteString(" {\n")
		builder.WriteString("\treverse_proxy ")
		builder.WriteString(upstreamHost)
		builder.WriteString(":")
		builder.WriteString(portString(route.Port))
		builder.WriteString("\n")
		builder.WriteString("}\n\n")
	}

	return builder.String()
}

type LocalCommandExecutor struct{}

func (LocalCommandExecutor) Run(ctx context.Context, command CaddyCommand) error {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s failed: %w\n%s", command.Name, strings.Join(command.Args, " "), err, output)
	}

	return nil
}

func routesByDomain(routes []Route) map[string]Route {
	result := map[string]Route{}
	for _, route := range routes {
		result[route.DomainName] = route
	}
	return result
}

func copyRoutes(routes map[string]Route) map[string]Route {
	copied := map[string]Route{}
	for domain, route := range routes {
		copied[domain] = route
	}
	return copied
}

func routesFromMap(routeMap map[string]Route) []Route {
	domains := make([]string, 0, len(routeMap))
	for domain := range routeMap {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	routes := make([]Route, 0, len(domains))
	for _, domain := range domains {
		routes = append(routes, routeMap[domain])
	}
	return routes
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

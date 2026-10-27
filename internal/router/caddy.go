package router

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
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
	ConfigPath     string
	MainConfigPath string
	Executor       CaddyCommandExecutor
	UpstreamHost   string
	AdminAddress   string
}

type CaddyRouter struct {
	configPath     string
	mainConfigPath string
	executor       CaddyCommandExecutor
	upstreamHost   string
	adminAddress   string
	activeClient   *http.Client
}

func NewCaddyRouter(config CaddyRouterConfig) *CaddyRouter {
	if config.MainConfigPath == "" {
		config.MainConfigPath = config.ConfigPath
	}
	if config.UpstreamHost == "" {
		config.UpstreamHost = "127.0.0.1"
	}

	return &CaddyRouter{
		configPath:     config.ConfigPath,
		mainConfigPath: config.MainConfigPath,
		executor:       config.Executor,
		upstreamHost:   config.UpstreamHost,
		adminAddress:   config.AdminAddress,
		activeClient:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (r *CaddyRouter) SyncRoutes(ctx context.Context, routes []Route) error {
	routeMap := routesByDomain(routes)
	if err := r.writeConfig(ctx, routeMap); err != nil {
		return err
	}

	return r.reload(ctx)
}

func (r *CaddyRouter) reload(ctx context.Context) error {
	if r.executor == nil {
		return nil
	}

	args := []string{"reload", "--config", r.mainConfigPath, "--adapter", "caddyfile"}
	if r.adminAddress != "" {
		args = append(args, "--address", r.adminAddress)
	}
	return r.executor.Run(ctx, CaddyCommand{Name: "caddy", Args: args})
}

func (r *CaddyRouter) Routes(ctx context.Context) ([]Route, error) {
	return r.readActiveRoutes(ctx)
}

func (r *CaddyRouter) writeConfig(ctx context.Context, routes map[string]Route) error {
	if err := os.MkdirAll(filepath.Dir(r.configPath), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(r.configPath), ".sshdock-caddy-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	adminAddress := ""
	if filepath.Clean(r.mainConfigPath) == filepath.Clean(r.configPath) {
		adminAddress = r.adminAddress
	}
	if _, err := tmp.WriteString(renderCaddyfile(routes, r.upstreamHost, adminAddress)); err != nil {
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

	return r.executor.Run(ctx, CaddyCommand{Name: "caddy", Args: []string{"validate", "--config", configPath, "--adapter", "caddyfile"}})
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
	if len(domains) == 0 && adminAddress == "" {
		builder.WriteString("# SSHDock has no active routes.\n")
		return builder.String()
	}
	for _, domain := range domains {
		route := routes[domain]
		builder.WriteString(domain)
		builder.WriteString(" {\n")
		builder.WriteString("\treverse_proxy ")
		builder.WriteString(upstreamHost)
		builder.WriteString(":")
		builder.WriteString(strconv.Itoa(route.Port))
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

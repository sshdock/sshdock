package router

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
)

const defaultCaddyAdminAddress = "localhost:2019"

type activeCaddyConfig struct {
	Apps activeCaddyApps `json:"apps"`
}

type activeCaddyApps struct {
	HTTP activeCaddyHTTP `json:"http"`
}

type activeCaddyHTTP struct {
	Servers map[string]activeCaddyServer `json:"servers"`
}

type activeCaddyServer struct {
	Listen         []string             `json:"listen"`
	AutomaticHTTPS activeAutomaticHTTPS `json:"automatic_https"`
	Routes         []activeCaddyRoute   `json:"routes"`
}

type activeAutomaticHTTPS struct {
	Skip []string `json:"skip"`
}

type activeCaddyRoute struct {
	Match  []activeCaddyMatch   `json:"match"`
	Handle []activeCaddyHandler `json:"handle"`
}

type activeCaddyMatch struct {
	Host []string `json:"host"`
}

type activeCaddyHandler struct {
	Handler   string                `json:"handler"`
	Routes    []activeCaddyRoute    `json:"routes"`
	Upstreams []activeCaddyUpstream `json:"upstreams"`
}

type activeCaddyUpstream struct {
	Dial string `json:"dial"`
}

func (r *CaddyRouter) readActiveRoutes(ctx context.Context) ([]Route, error) {
	address := r.adminAddress
	if address == "" {
		address = defaultCaddyAdminAddress
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/config/", nil)
	if err != nil {
		return nil, fmt.Errorf("build Caddy active-config request: %w", err)
	}
	response, err := r.activeClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("read active Caddy config from %s: %w", address, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read active Caddy config from %s: HTTP %s", address, response.Status)
	}

	var config activeCaddyConfig
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&config); err != nil {
		return nil, fmt.Errorf("decode active Caddy config from %s: %w", address, err)
	}
	return routesFromActiveConfig(config), nil
}

func routesFromActiveConfig(config activeCaddyConfig) []Route {
	routes := make([]Route, 0)
	for _, server := range config.Apps.HTTP.Servers {
		listenPort, found := caddyListenPort(server.Listen)
		if !found {
			continue
		}
		for _, activeRoute := range server.Routes {
			upstreamPort, found := caddyUpstreamPort(activeRoute.Handle)
			if !found {
				continue
			}
			for _, host := range caddyRouteHosts(activeRoute.Match) {
				https := caddyRouteUsesHTTPS(server, host, listenPort)
				routes = append(routes, Route{
					DomainName: caddySiteAddress(host, listenPort, https),
					Port:       upstreamPort,
					HTTPS:      https,
				})
			}
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].DomainName < routes[j].DomainName
	})
	return routes
}

func caddyListenPort(addresses []string) (int, bool) {
	for _, address := range addresses {
		if port, found := caddyEndpointPort(address); found {
			return port, true
		}
	}
	return 0, false
}

func caddyUpstreamPort(handlers []activeCaddyHandler) (int, bool) {
	for _, handler := range handlers {
		if handler.Handler == "reverse_proxy" {
			for _, upstream := range handler.Upstreams {
				if port, found := caddyEndpointPort(upstream.Dial); found {
					return port, true
				}
			}
		}
		for _, route := range handler.Routes {
			if port, found := caddyUpstreamPort(route.Handle); found {
				return port, true
			}
		}
	}
	return 0, false
}

func caddyEndpointPort(address string) (int, bool) {
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		return 0, false
	}
	port, err := strconv.Atoi(portText)
	return port, err == nil
}

func caddyRouteHosts(matches []activeCaddyMatch) []string {
	hosts := make([]string, 0)
	for _, match := range matches {
		hosts = append(hosts, match.Host...)
	}
	return hosts
}

func caddyRouteUsesHTTPS(server activeCaddyServer, host string, listenPort int) bool {
	for _, skippedHost := range server.AutomaticHTTPS.Skip {
		if skippedHost == host {
			return false
		}
	}
	return listenPort != 80
}

func caddySiteAddress(host string, listenPort int, https bool) string {
	if https && listenPort == 443 {
		return host
	}
	if !https && listenPort == 80 {
		return "http://" + host
	}
	scheme := "http://"
	if https {
		scheme = "https://"
	}
	return scheme + net.JoinHostPort(host, strconv.Itoa(listenPort))
}

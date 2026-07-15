package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

func (b *StoreBackend) CheckDomains(appName string) ([]DomainCheck, error) {
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
	failures, err := b.store.ListRouteApplyFailuresByApp(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list route apply failures for app %q: %w", appName, err)
	}
	failuresByDomain := make(map[string]store.RouteApplyFailure, len(failures))
	for _, failure := range failures {
		failuresByDomain[failure.DomainName] = failure
	}
	routesByDomain := map[string]router.Route{}
	routerInspectionSupported := false
	var routerReadErr error
	if reader, ok := b.router.(routeReader); ok {
		routes, err := reader.Routes(ctx)
		if err != nil {
			routerReadErr = err
		} else {
			for _, route := range routes {
				routesByDomain[routeComparisonKey(route.DomainName)] = route
			}
		}
		routerInspectionSupported = true
	}

	checks := make([]DomainCheck, 0, len(models)+len(failures))
	desiredDomains := make(map[string]struct{}, len(models))
	for _, model := range models {
		desiredDomains[model.DomainName] = struct{}{}
		check := DomainCheck{
			DomainName:  model.DomainName,
			ServiceName: model.ServiceName,
			Port:        model.Port,
			HTTPS:       model.HTTPS,
			Status:      "stored",
			Detail:      "router check unavailable",
		}
		failure, applyFailed := failuresByDomain[model.DomainName]
		switch {
		case routerReadErr != nil:
			check.Status = "unavailable"
			check.Detail = "active Caddy check failed: " + routerReadErr.Error() + "; run sudo sshdock diagnostics and check Caddy"
			if applyFailed {
				check.Detail += "; last apply: " + routeApplyFailureDetail(failure)
			}
		case !routerInspectionSupported && applyFailed:
			check.Status = "failed"
			check.Detail = routeApplyFailureDetail(failure)
		case routerInspectionSupported:
			route, ok := routesByDomain[routeComparisonKey(model.DomainName)]
			switch {
			case ok && route.Port == model.Port && route.HTTPS == model.HTTPS:
				check.Status = "ok"
				check.Detail = "active Caddy route matches"
			case applyFailed:
				check.Status = "failed"
				check.Detail = routeApplyFailureDetail(failure)
			case !ok:
				check.Status = "missing"
				check.Detail = "active Caddy route missing"
			default:
				check.Status = "mismatch"
				check.Detail = "router route differs"
			}
		}
		checks = append(checks, check)
	}
	for _, failure := range failures {
		if _, desired := desiredDomains[failure.DomainName]; desired {
			continue
		}
		activeRoute, active := routesByDomain[routeComparisonKey(failure.DomainName)]
		if routerInspectionSupported && routerReadErr == nil && !active {
			continue
		}
		check := DomainCheck{
			DomainName:  failure.DomainName,
			ServiceName: failure.ServiceName,
			Port:        failure.Port,
			HTTPS:       failure.HTTPS,
			Status:      "failed",
			Detail:      routeApplyFailureDetail(failure),
		}
		if routerReadErr != nil {
			check.Status = "unavailable"
			check.Detail += "; active Caddy check failed: " + routerReadErr.Error()
		} else if active && (activeRoute.Port != failure.Port || activeRoute.HTTPS != failure.HTTPS) {
			check.Detail += "; active Caddy route also differs"
		}
		checks = append(checks, check)
	}
	sort.Slice(checks, func(i, j int) bool {
		return checks[i].DomainName < checks[j].DomainName
	})
	return checks, nil
}

func routeApplyFailureDetail(failure store.RouteApplyFailure) string {
	return string(failure.Operation) + " failed to apply: " + failure.Detail
}

func routeComparisonKey(domainName string) string {
	parsed, err := url.Parse(domainName)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return domainName
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return domainName
	}
	return strings.ToLower(parsed.Hostname())
}

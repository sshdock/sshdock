package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sshdock/sshdock/internal/compose"
)

func hookRunnerFromEnv() (compose.Runner, error) {
	runner := os.Getenv("SSHDOCK_COMPOSE_RUNNER")
	if runner == "" || runner == "fake" {
		return fakeRunnerFromEnv(), nil
	}
	if runner == "docker" {
		return compose.NewDockerRunner(compose.LocalCommandExecutor{}), nil
	}

	return nil, fmt.Errorf("unsupported SSHDOCK_COMPOSE_RUNNER %q", runner)
}

func dashboardRunnerFromEnv() (compose.Runner, error) {
	runner := os.Getenv("SSHDOCK_COMPOSE_RUNNER")
	if runner == "" || runner == "fake" {
		fake := fakeRunnerFromEnv()
		fake.Services = parseFakeServices(os.Getenv("SSHDOCK_FAKE_COMPOSE_SERVICES"))
		fake.LogOutput = os.Getenv("SSHDOCK_FAKE_COMPOSE_LOGS")
		return fake, nil
	}
	if runner == "docker" {
		return compose.NewDockerRunner(compose.LocalCommandExecutor{}), nil
	}

	return nil, fmt.Errorf("unsupported SSHDOCK_COMPOSE_RUNNER %q", runner)
}

func fakeRunnerFromEnv() *compose.FakeRunner {
	deployResult, routeErr := parseFakeDeployResult(
		os.Getenv("SSHDOCK_FAKE_COMPOSE_ROUTE"),
		os.Getenv("SSHDOCK_FAKE_COMPOSE_ROUTE_REASON"),
	)
	return &compose.FakeRunner{
		DeployResult: deployResult,
		DeployErr:    errors.Join(envError("SSHDOCK_FAKE_COMPOSE_DEPLOY_ERROR"), routeErr),
		RestartErr:   envError("SSHDOCK_FAKE_COMPOSE_RESTART_ERROR"),
		RemoveErr:    envError("SSHDOCK_FAKE_COMPOSE_REMOVE_ERROR"),
	}
}

func parseFakeDeployResult(route string, reason string) (compose.DeployResult, error) {
	if route == "" {
		return compose.DeployResult{RouteReason: reason}, nil
	}
	serviceName, portText, found := strings.Cut(route, ":")
	port, err := strconv.Atoi(portText)
	if !found || serviceName == "" || err != nil || port < 1 || port > 65535 {
		return compose.DeployResult{}, fmt.Errorf("SSHDOCK_FAKE_COMPOSE_ROUTE %q must be service:port", route)
	}
	return compose.DeployResult{
		RouteFound:  true,
		RouteTarget: compose.RouteTarget{ServiceName: serviceName, Port: port},
	}, nil
}

func envError(name string) error {
	value := os.Getenv(name)
	if value == "" {
		return nil
	}
	return errors.New(value)
}

func parseFakeServices(value string) []compose.ServiceStatus {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	services := make([]compose.ServiceStatus, 0, len(parts))
	for _, part := range parts {
		name, state, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok || name == "" {
			continue
		}
		if state == "" {
			state = "unknown"
		}
		services = append(services, compose.ServiceStatus{Name: name, State: state})
	}
	return services
}

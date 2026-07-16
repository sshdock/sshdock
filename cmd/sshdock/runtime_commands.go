package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sshdock/sshdock/internal/compose"
)

func commandNeedsStore(args []string) bool {
	if commandIsHelpRequest(args) {
		return false
	}
	if len(args) >= 1 && args[0] == "config" {
		return true
	}
	if len(args) == 2 && args[0] == "apps" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "apps" && (args[1] == "create" || args[1] == "info" || args[1] == "health") {
		return true
	}
	if len(args) >= 3 && len(args) <= 4 && args[0] == "apps" && args[1] == "remove" {
		return true
	}
	if commandNeedsRecoveryRunner(args) {
		return true
	}
	if len(args) == 3 && args[0] == "releases" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "deployments" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "events" && args[1] == "list" {
		return true
	}
	if len(args) == 7 && args[0] == "domains" && args[1] == "attach" && args[5] == "--port" {
		_, err := strconv.Atoi(args[6])
		return err == nil
	}
	if len(args) == 3 && args[0] == "domains" && (args[1] == "list" || args[1] == "check") {
		return true
	}
	if len(args) == 4 && args[0] == "domains" && args[1] == "detach" {
		return true
	}
	if len(args) == 4 && args[0] == "server" && args[1] == "domain" && args[2] == "set" {
		return true
	}
	if len(args) == 2 && args[0] == "ssh-keys" && args[1] == "list" {
		return true
	}
	if len(args) == 3 && args[0] == "ssh-keys" && (args[1] == "add" || args[1] == "remove") {
		return true
	}

	return false
}

func commandIsHelpRequest(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if len(args) == 1 {
		return args[0] == "help" || args[0] == "-h" || args[0] == "--help"
	}
	if args[0] == "help" {
		return true
	}
	return len(args) == 2 && (args[1] == "-h" || args[1] == "--help")
}

func commandNeedsRecoveryRunner(args []string) bool {
	if len(args) >= 2 && args[0] == "logs" {
		return true
	}
	if len(args) == 3 && args[0] == "apps" && args[1] == "health" {
		return true
	}
	if len(args) == 3 && args[0] == "apps" && (args[1] == "start" || args[1] == "stop" || args[1] == "restart" || args[1] == "redeploy") {
		return true
	}
	if len(args) == 4 && args[0] == "apps" && (args[1] == "restart" || args[1] == "rollback") {
		return true
	}
	if len(args) >= 6 && args[0] == "apps" && (args[1] == "exec" || args[1] == "run") && args[4] == "--" {
		return true
	}
	if len(args) >= 3 && len(args) <= 4 && args[0] == "apps" && args[1] == "remove" {
		return true
	}
	return false
}

func cliRunnerFromEnv() (compose.Runner, error) {
	runner := os.Getenv("SSHDOCK_COMPOSE_RUNNER")
	if runner == "" || runner == "docker" {
		return compose.NewDockerRunner(compose.LocalCommandExecutor{}), nil
	}
	if runner == "fake" {
		return &compose.FakeRunner{
			DeployErr:  envError("SSHDOCK_FAKE_COMPOSE_DEPLOY_ERROR"),
			StartErr:   envError("SSHDOCK_FAKE_COMPOSE_START_ERROR"),
			StopErr:    envError("SSHDOCK_FAKE_COMPOSE_STOP_ERROR"),
			RestartErr: envError("SSHDOCK_FAKE_COMPOSE_RESTART_ERROR"),
			ExecErr:    envError("SSHDOCK_FAKE_COMPOSE_EXEC_ERROR"),
			RunErr:     envError("SSHDOCK_FAKE_COMPOSE_RUN_ERROR"),
			RemoveErr:  envError("SSHDOCK_FAKE_COMPOSE_REMOVE_ERROR"),
			ExecOutput: os.Getenv("SSHDOCK_FAKE_COMPOSE_EXEC_OUTPUT"),
			RunOutput:  os.Getenv("SSHDOCK_FAKE_COMPOSE_RUN_OUTPUT"),
			Services:   parseFakeServices(os.Getenv("SSHDOCK_FAKE_COMPOSE_SERVICES")),
			LogOutput:  os.Getenv("SSHDOCK_FAKE_COMPOSE_LOGS"),
		}, nil
	}

	return nil, fmt.Errorf("unsupported SSHDOCK_COMPOSE_RUNNER %q", runner)
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
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		services = append(services, compose.ServiceStatus{
			Name:  strings.TrimSpace(name),
			State: strings.TrimSpace(state),
		})
	}
	return services
}

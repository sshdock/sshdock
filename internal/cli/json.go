package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrNotFound = errors.New("not found")

type outputError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *outputError) Error() string { return e.Message }

func (e *outputError) Is(target error) bool {
	return target == ErrNotFound && e.Code == "not_found"
}

func JSONError(code string, message string) error {
	return &outputError{Code: code, Message: message}
}

// ParseOutputArgs recognizes only trailing --json on supported read commands.
func ParseOutputArgs(args []string) ([]string, bool, error) {
	limit := len(args)
	if len(args) >= 5 && args[0] == "apps" && (args[1] == "exec" || args[1] == "run") && args[4] == "--" {
		limit = 4
	}
	jsonCount := 0
	for _, arg := range args[:limit] {
		if arg == "--json" {
			jsonCount++
		}
	}
	if jsonCount == 0 {
		return args, false, nil
	}
	if jsonCount != 1 || args[len(args)-1] != "--json" || limit != len(args) {
		return args, true, JSONError("invalid_arguments", "--json must appear once at the end of a supported read command")
	}
	normalized := args[:len(args)-1]
	if !jsonCommandAllowed(normalized) {
		return normalized, true, JSONError("invalid_arguments", "--json is supported only for check, apps list/info/health/check, releases list, deployments list/info, domains list/check, events list, and diagnostics")
	}
	return normalized, true, nil
}

func jsonCommandAllowed(args []string) bool {
	if len(args) == 1 {
		return args[0] == "diagnostics" || args[0] == "check"
	}
	if len(args) < 2 {
		return false
	}
	for _, arg := range args[2:] {
		if arg == "" || strings.HasPrefix(arg, "-") {
			return false
		}
	}
	switch args[0] {
	case "check":
		return len(args) == 2 && args[1] != "" && !strings.HasPrefix(args[1], "-")
	case "apps":
		return len(args) == 2 && args[1] == "list" || len(args) == 3 && (args[1] == "info" || args[1] == "health" || args[1] == "check")
	case "releases", "events":
		return len(args) == 3 && args[1] == "list"
	case "deployments":
		return len(args) == 3 && args[1] == "list" || len(args) == 4 && args[1] == "info"
	case "domains":
		return len(args) == 3 && (args[1] == "list" || args[1] == "check")
	}
	return false
}

func JSONCommandName(args []string) string {
	if len(args) == 0 {
		return "unknown"
	}
	switch args[0] {
	case "apps", "releases", "deployments", "domains", "events":
		if len(args) > 1 {
			switch args[1] {
			case "list", "info", "health", "check":
				return args[0] + "." + args[1]
			}
		}
		return args[0]
	case "check", "diagnostics", "config", "logs", "backup", "server", "ssh-keys", "help", "version":
		return args[0]
	}
	return "unknown"
}

func WriteJSON(stdout io.Writer, command string, data any, err error) error {
	var failure *outputError
	if err != nil {
		data = nil
		failure = &outputError{Code: "unavailable", Message: err.Error()}
		var explicit *outputError
		if errors.As(err, &explicit) {
			failure.Code = explicit.Code
		} else if errors.Is(err, ErrNotFound) {
			failure.Code = "not_found"
		}
	}
	return json.NewEncoder(stdout).Encode(struct {
		SchemaVersion string       `json:"schema_version"`
		Command       string       `json:"command"`
		Data          any          `json:"data"`
		Error         *outputError `json:"error"`
	}{SchemaVersion: "1", Command: command, Data: data, Error: failure})
}

func jsonList[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func (r *Runner) runJSON(args []string, stdout io.Writer, stderr io.Writer) int {
	command := JSONCommandName(args)
	var data any
	var err error
	exitCode := 0
	if command == "check" || command == "diagnostics" {
		err = JSONError("invalid_arguments", "unsupported structured command")
	} else if err = r.ensureBackend(); err == nil {
		switch command {
		case "apps.list":
			var apps []App
			apps, err = r.backend.ListApps()
			data = jsonList(apps)
		case "apps.info":
			data, err = r.backend.GetApp(args[2])
		case "apps.health":
			var report AppHealth
			report, err = r.backend.AppHealth(args[2])
			report.Checks = jsonList(report.Checks)
			report.Services = jsonList(report.Services)
			data = report
		case "apps.check":
			report, checkErr := r.appPreflight(args[2])
			err = checkErr
			report.Checks = jsonList(report.Checks)
			data = report
			if !report.OK {
				exitCode = 1
			}
		case "releases.list":
			var releases []Release
			releases, err = r.backend.ListReleases(args[2])
			data = jsonList(releases)
		case "deployments.list":
			var deployments []Deployment
			deployments, err = r.backend.ListDeployments(args[2])
			data = jsonList(deployments)
		case "deployments.info":
			data, err = r.deploymentInfo(args[2], args[3])
		case "domains.list":
			var domains []Domain
			domains, err = r.backend.ListDomains(args[2])
			data = jsonList(domains)
		case "domains.check":
			var checks []DomainCheck
			checks, err = r.backend.CheckDomains(args[2])
			data = jsonList(checks)
		case "events.list":
			var events []Event
			events, err = r.backend.ListEvents(args[2])
			data = jsonList(events)
		default:
			err = JSONError("invalid_arguments", "unsupported structured command")
		}
	}
	if writeErr := WriteJSON(stdout, command, data, err); writeErr != nil {
		fmt.Fprintln(stderr, writeErr)
		return 1
	}
	if err != nil {
		var failure *outputError
		if errors.As(err, &failure) && failure.Code == "invalid_arguments" {
			return 2
		}
		return 1
	}
	return exitCode
}

func (r *Runner) deploymentInfo(appName string, deploymentID string) (Deployment, error) {
	deployments, err := r.backend.ListDeployments(appName)
	if err != nil {
		return Deployment{}, err
	}
	for _, deployment := range deployments {
		if deployment.ID == deploymentID {
			return deployment, nil
		}
	}
	return Deployment{}, JSONError("not_found", fmt.Sprintf("deployment %q not found for app %q", deploymentID, appName))
}

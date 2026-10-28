package compose

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/template"
)

type PreflightRequest struct {
	AppName         string
	ProjectDir      string
	Env             map[string]string
	Scope           string
	RedactionValues map[string]string
}

type PreflightReport struct {
	OK          bool             `json:"ok"`
	Scope       string           `json:"scope"`
	ComposePath string           `json:"compose_path"`
	Checks      []PreflightCheck `json:"checks"`
}

type PreflightCheck struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix"`
	Blocking bool   `json:"blocking"`
}

func (r *DockerRunner) Preflight(ctx context.Context, request PreflightRequest) (report PreflightReport, err error) {
	if request.Scope != "local" && request.Scope != "server" {
		return report, fmt.Errorf("preflight scope must be local or server")
	}
	projectName, err := ProjectName(request.AppName)
	if err != nil {
		return report, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	report = PreflightReport{OK: true, Scope: request.Scope, Checks: []PreflightCheck{}}
	redactions := maps.Clone(request.RedactionValues)
	if redactions == nil {
		redactions = map[string]string{}
	}
	defer func() {
		report.ComposePath = RedactValues(report.ComposePath, redactions)
		for index := range report.Checks {
			report.Checks[index].Name = RedactValues(report.Checks[index].Name, redactions)
			report.Checks[index].Detail = RedactValues(report.Checks[index].Detail, redactions)
			report.Checks[index].Fix = RedactValues(report.Checks[index].Fix, redactions)
		}
		err = RedactError(err, redactions)
	}()
	report.add("images", "unknown", "Image pulls, private registry access and application builds were not attempted.", "Verify registry credentials and build inputs before deploying.", false)
	report.add("public reachability", "unknown", "DNS propagation, firewall rules and public HTTP/HTTPS availability were not verified.", "Run sshdock diagnostics on the server and verify the public route separately.", false)
	if request.Scope == "local" {
		report.add("server runtime", "unknown", "This checks local source; the target server and its Docker runtime were not observed.", "Run ssh sshdock@<host> apps check <app> for server observations.", false)
		report.add("server ports", "unknown", "Workstation port availability does not establish target-server port availability.", "Run apps check <app> on the target server.", false)
	}
	request.ProjectDir, err = filepath.Abs(request.ProjectDir)
	if err != nil {
		return report, err
	}
	path, detectErr := DetectFile(request.ProjectDir)
	if detectErr != nil {
		report.add("Compose file", "fail", detectErr.Error(), "Keep exactly one regular root compose.yaml, compose.yml, docker-compose.yaml or docker-compose.yml.", true)
		return report, nil
	}
	report.ComposePath = path
	report.add("Compose file", "ok", "Exactly one supported root Compose file was found.", "", false)
	rawModel, loadErr := loadComposeModel(path)
	if loadErr != nil {
		report.add("Compose policy", "fail", loadErr.Error(), "Fix the selected Compose YAML, then run the check again.", true)
		return report, nil
	}
	request.Env = maps.Clone(request.Env)
	if request.Env == nil {
		request.Env = map[string]string{}
	}
	if request.Scope == "local" {
		// Inherited hook/repository variables must not select another worktree.
		gitArgs := []string{}
		for _, entry := range os.Environ() {
			name, _, _ := strings.Cut(entry, "=")
			if strings.HasPrefix(name, "GIT_") {
				gitArgs = append(gitArgs, "-u", name)
			}
		}
		gitArgs = append(gitArgs, "git", "rev-parse", "--verify", "HEAD^{commit}")
		sha, _, gitErr := r.preflightCommand(ctx, Command{Name: "env", Dir: request.ProjectDir, Args: gitArgs})
		sha = strings.TrimSpace(sha)
		decoded, decodeErr := hex.DecodeString(sha)
		request.Env[GitSHAEnv] = ""
		if gitErr == nil && decodeErr == nil && (len(decoded) == 20 || len(decoded) == 32) {
			request.Env[GitSHAEnv] = strings.ToLower(sha)
			report.add("commit metadata", "ok", "Local Git HEAD supplies SSHDOCK_GIT_SHA: "+strings.ToLower(sha), "", false)
		} else {
			report.add("commit metadata", "unknown", "Local Git HEAD could not be verified; SSHDOCK_GIT_SHA is unavailable and environment overrides are ignored.", "Commit the project in a Git worktree, or check the managed app on the server.", false)
		}
	} else if _, supplied := request.Env[GitSHAEnv]; !supplied {
		request.Env[GitSHAEnv] = ""
	}
	environment, envErr := interpolationEnvironment(path, request.Env)
	if envErr != nil {
		report.add("Compose environment", "fail", "Compose interpolation environment could not be loaded; environment file contents are withheld.", "Check .env and COMPOSE_ENV_FILES paths, syntax and read permissions.", true)
		return report, nil
	}
	for name := range template.ExtractVariables(plainComposeValue(rawModel).(map[string]any), nil) {
		if name != GitSHAEnv {
			redactions["interpolation:"+name] = environment[name]
		}
	}
	if _, validationErr := validateResolvedComposeModel(path, rawModel, environment); validationErr != nil {
		report.add("Compose policy", "fail", validationErr.Error(), "Fix the named interpolation variable or single-file Compose policy; configure server values with sshdock config set.", true)
		return report, nil
	}
	_, _, versionErr := r.preflightCommand(ctx, Command{Name: "docker", Dir: request.ProjectDir, Env: request.Env, Args: []string{"compose", "version"}})
	if versionErr != nil {
		status, blocking := "unknown", false
		if request.Scope == "server" {
			status, blocking = "fail", true
		}
		report.add("native Compose validation", status, "Docker Compose is unavailable; native configuration validation was not completed: "+versionErr.Error(), "Install or repair the Docker Compose plugin; run docker compose version.", blocking)
		return report, ctx.Err()
	}
	command := Command{Name: "docker", Dir: request.ProjectDir, Env: request.Env, Args: commandArgs(composeArgs(path, projectName), "config", "--format", "json")}
	output, warnings, configErr := r.preflightCommand(ctx, command)
	if configErr != nil {
		report.add("native Compose validation", "fail", configErr.Error(), "Run docker compose -f '"+strings.ReplaceAll(path, "'", "'\\''")+"' config on this host and fix the reported configuration.", true)
		return report, ctx.Err()
	}
	result, model, modelErr := analyzeEffectiveModel(output, projectName)
	if modelErr != nil {
		report.add("native Compose validation", "unknown", "Docker Compose returned an unreadable effective model; configuration validity could not be established.", "Check docker compose version and docker compose config --format json.", false)
		return report, nil
	}
	report.add("native Compose validation", "ok", "Docker Compose accepted this configuration on the checking host; no images were pulled or built and no containers were changed.", "", false)
	if strings.TrimSpace(warnings) != "" {
		report.add("Compose warnings", "warn", strings.TrimSpace(warnings), "Review the Docker Compose warning and configure any named values before deploying.", false)
	}
	if result.RouteFound {
		report.add("routing", "ok", fmt.Sprintf("Automatic routing selects service %s on published TCP port %d; public availability is unverified.", result.RouteTarget.ServiceName, result.RouteTarget.Port), "", false)
	} else {
		report.add("routing", "warn", result.RouteReason+"; the app may deploy without an automatic HTTP route.", "Use one published TCP port on web, or attach the intended service with sshdock domains attach after deployment.", false)
	}
	for _, warning := range result.Warnings {
		report.add("Compose configuration", "warn", warning, "Review the trusted Compose configuration and keep routed ports on loopback when appropriate.", false)
	}
	ports := report.preflightPublishedPorts(model)
	if request.Scope == "server" {
		_, _, runtimeErr := r.preflightCommand(ctx, Command{Name: "docker", Dir: request.ProjectDir, Env: request.Env, Args: []string{"info", "--format", "{{.ServerVersion}}"}})
		if runtimeErr != nil {
			report.add("Docker runtime", "fail", "The configured Docker daemon is inaccessible: "+runtimeErr.Error(), "Run docker info; check Docker service, context and user permissions.", true)
		} else {
			report.add("Docker runtime", "ok", "The configured Docker daemon is accessible; this does not prove an application will build or start.", "", false)
		}
		r.preflightServerPorts(ctx, request, projectName, ports, runtimeErr == nil, &report)
	}
	return report, ctx.Err()
}

func (r *PreflightReport) add(name, status, detail, fix string, blocking bool) {
	r.Checks = append(r.Checks, PreflightCheck{Name: name, Status: status, Detail: detail, Fix: fix, Blocking: blocking})
	if blocking {
		r.OK = false
	}
}

func (r *DockerRunner) preflightCommand(ctx context.Context, command Command) (string, string, error) {
	if r == nil || r.executor == nil {
		return "", "", fmt.Errorf("command executor is not configured")
	}
	if streamer, ok := r.executor.(streamingCommandExecutor); ok {
		var stdout, stderr bytes.Buffer
		err := streamer.Stream(ctx, command, &stdout, &stderr)
		if err != nil && strings.TrimSpace(stderr.String()) != "" {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), stderr.String(), err
	}
	output, err := r.executor.Run(ctx, command)
	return output, "", err
}

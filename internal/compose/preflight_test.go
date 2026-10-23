package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const preflightModel = `{"services":{"web":{"image":"example/web","ports":[{"host_ip":"127.0.0.1","published":"3100","target":80,"protocol":"tcp"}]}}}`

type preflightResponse struct {
	stdout, stderr string
	err            error
}

type preflightExecutor struct {
	t         *testing.T
	commands  []Command
	responses map[string]preflightResponse
}

func (e *preflightExecutor) response(ctx context.Context, command Command) preflightResponse {
	e.t.Helper()
	e.commands = append(e.commands, command)
	if _, deadline := ctx.Deadline(); !deadline {
		e.t.Error("preflight command has no deadline")
	}
	key := command.Name + " " + strings.Join(command.Args, " ")
	if command.Name == "env" {
		args := command.Args
		for len(args) >= 2 && args[0] == "-u" && strings.HasPrefix(args[1], "GIT_") {
			args = args[2:]
		}
		if reflect.DeepEqual(args, []string{"git", "rev-parse", "--verify", "HEAD^{commit}"}) {
			key = strings.Join(args, " ")
		}
	}
	allowed := key == "git rev-parse --verify HEAD^{commit}" || key == "docker compose version" || key == "docker info --format {{.ServerVersion}}" || key == "docker ps --quiet" || key == "ss -H -ltn" || key == "ss -H -lun" || key == "docker inspect abc123"
	if command.Name == "docker" && len(command.Args) == 8 && reflect.DeepEqual(command.Args[5:], []string{"config", "--format", "json"}) {
		allowed = command.Args[0] == "compose" && command.Args[1] == "-f" && command.Args[3] == "-p"
		key = "config"
	}
	if !allowed {
		e.t.Errorf("preflight issued non-allowlisted command: %#v", command)
		return preflightResponse{err: errors.New("command not allowed")}
	}
	response, found := e.responses[key]
	if !found {
		e.t.Errorf("unexpected read command: %#v", command)
		return preflightResponse{err: errors.New("unexpected command")}
	}
	return response
}

func (e *preflightExecutor) Run(ctx context.Context, command Command) (string, error) {
	response := e.response(ctx, command)
	return response.stdout, response.err
}

func (e *preflightExecutor) Stream(ctx context.Context, command Command, stdout io.Writer, stderr io.Writer) error {
	response := e.response(ctx, command)
	if _, err := io.WriteString(stdout, response.stdout); err != nil {
		return err
	}
	if _, err := io.WriteString(stderr, response.stderr); err != nil {
		return err
	}
	return response.err
}

func preflightFixture(t *testing.T, scope string) (PreflightRequest, *preflightExecutor) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yml"), "services:\n  web:\n    image: example/web\n    ports: [\"127.0.0.1:3100:80\"]\n")
	return PreflightRequest{AppName: "my-app", ProjectDir: dir, Scope: scope, Env: map[string]string{GitSHAEnv: strings.Repeat("b", 40)}}, &preflightExecutor{t: t, responses: map[string]preflightResponse{
		"git rev-parse --verify HEAD^{commit}":    {stdout: strings.Repeat("a", 40) + "\n"},
		"docker compose version":                  {stdout: "Docker Compose version v2.0.0"},
		"config":                                  {stdout: preflightModel},
		"docker info --format {{.ServerVersion}}": {stdout: "available"},
		"docker ps --quiet":                       {},
		"docker inspect abc123":                   {stdout: "[]"},
		"ss -H -ltn":                              {},
		"ss -H -lun":                              {},
	}}
}

func findPreflightCheck(t *testing.T, report PreflightReport, name, status string) PreflightCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name && check.Status == status {
			return check
		}
	}
	t.Fatalf("missing %s %s check: %#v", status, name, report.Checks)
	return PreflightCheck{}
}

func TestPreflightLocalChecksSourceWithoutMutatingOrClaimingServerReadiness(t *testing.T) {
	request, executor := preflightFixture(t, "local")
	path := filepath.Join(request.ProjectDir, "compose.yml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	envBefore := request.Env[GitSHAEnv]
	report, err := NewDockerRunner(executor).Preflight(context.Background(), request)
	if err != nil || !report.OK || report.Scope != "local" || report.ComposePath != path {
		t.Fatalf("report=%#v error=%v", report, err)
	}
	findPreflightCheck(t, report, "native Compose validation", "ok")
	findPreflightCheck(t, report, "routing", "ok")
	if check := findPreflightCheck(t, report, "commit metadata", "ok"); !strings.Contains(check.Detail, strings.Repeat("a", 40)) {
		t.Fatalf("verified public Git SHA was hidden: %#v", check)
	}
	for _, name := range []string{"server runtime", "server ports", "images", "public reachability"} {
		if check := findPreflightCheck(t, report, name, "unknown"); check.Blocking || check.Fix == "" {
			t.Fatalf("unobserved check = %#v", check)
		}
	}
	if len(executor.commands) != 3 || executor.commands[2].Env[GitSHAEnv] != strings.Repeat("a", 40) || executor.commands[2].Dir != request.ProjectDir {
		t.Fatalf("commands = %#v", executor.commands)
	}
	after, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) || request.Env[GitSHAEnv] != envBefore {
		t.Fatalf("preflight changed source or caller environment: %v", err)
	}
	entries, err := os.ReadDir(request.ProjectDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "compose.yml" {
		t.Fatalf("preflight created project files: %v %v", entries, err)
	}
}

func TestPreflightRejectsMissingMultipleInvalidAndRequiredConfig(t *testing.T) {
	for _, test := range []struct {
		name, content, check string
		remove, multiple     bool
	}{
		{name: "missing", remove: true, check: "Compose file"},
		{name: "multiple", multiple: true, check: "Compose file"},
		{name: "invalid YAML", content: "services: [\n", check: "Compose policy"},
		{name: "missing required config", content: "services:\n  web:\n    image: ${SSHDOCK_PREFLIGHT_REQUIRED:?configure it}\n", check: "Compose policy"},
		{name: "external file policy", content: "include: other.yml\nservices: {}\n", check: "Compose policy"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, executor := preflightFixture(t, "server")
			if test.remove {
				if err := os.Remove(filepath.Join(request.ProjectDir, "compose.yml")); err != nil {
					t.Fatal(err)
				}
			}
			if test.multiple {
				writeFile(t, filepath.Join(request.ProjectDir, "compose.yaml"), "services: {}\n")
			}
			if test.content != "" {
				writeFile(t, filepath.Join(request.ProjectDir, "compose.yml"), test.content)
			}
			t.Setenv("SSHDOCK_PREFLIGHT_REQUIRED", "")
			report, err := NewDockerRunner(executor).Preflight(context.Background(), request)
			if err != nil || report.OK || !findPreflightCheck(t, report, test.check, "fail").Blocking || len(executor.commands) != 0 {
				t.Fatalf("report=%#v commands=%#v error=%v", report, executor.commands, err)
			}
		})
	}
}

func TestPreflightNativeValidationAndPrerequisites(t *testing.T) {
	for _, test := range []struct {
		name, scope, command, check, status string
		response                            preflightResponse
		blocking                            bool
	}{
		{"local missing Compose", "local", "docker compose version", "native Compose validation", "unknown", preflightResponse{err: errors.New("docker missing")}, false},
		{"server missing Compose", "server", "docker compose version", "native Compose validation", "fail", preflightResponse{err: errors.New("docker missing")}, true},
		{"native config rejection", "server", "config", "native Compose validation", "fail", preflightResponse{err: errors.New("invalid Compose config")}, true},
		{"unreadable native response", "server", "config", "native Compose validation", "unknown", preflightResponse{stdout: "not JSON"}, false},
		{"daemon unavailable", "server", "docker info --format {{.ServerVersion}}", "Docker runtime", "fail", preflightResponse{err: errors.New("permission denied")}, true},
		{"native warning", "local", "config", "Compose warnings", "warn", preflightResponse{stdout: preflightModel, stderr: "optional variable is not set"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, executor := preflightFixture(t, test.scope)
			executor.responses[test.command] = test.response
			report, err := NewDockerRunner(executor).Preflight(context.Background(), request)
			if err != nil || report.OK == test.blocking || findPreflightCheck(t, report, test.check, test.status).Blocking != test.blocking {
				t.Fatalf("report=%#v error=%v", report, err)
			}
		})
	}
}

func TestPreflightRedactsLocalEnvironmentAndCapturedNativeOutput(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprintf("native failure=%v", failure), func(t *testing.T) {
			request, executor := preflightFixture(t, "local")
			writeFile(t, filepath.Join(request.ProjectDir, "compose.yml"), "services:\n  web:\n    image: example/web\n    environment:\n      TOKEN: ${SSHDOCK_PREFLIGHT_PROCESS_TOKEN}\n      DOTENV: ${SSHDOCK_PREFLIGHT_FILE_TOKEN}\n      APP_CONFIG: ${APP_SECRET}\n")
			writeFile(t, filepath.Join(request.ProjectDir, ".env"), "SSHDOCK_PREFLIGHT_FILE_TOKEN=file-secret\nSSHDOCK_GIT_SHA=spoofed-env-revision\n")
			t.Setenv("SSHDOCK_PREFLIGHT_PROCESS_TOKEN", "process-secret")
			t.Setenv(GitSHAEnv, "spoofed-process-revision")
			request.Env["APP_SECRET"] = "configured-secret"
			request.RedactionValues = map[string]string{"APP_SECRET": "configured-secret"}
			response := preflightResponse{stdout: preflightModel, stderr: "warning process-secret file-secret configured-secret"}
			if failure {
				response.stdout = `{"environment":{"DO_NOT_PRINT_EFFECTIVE_MODEL":"configured-secret"}}`
				response.err = errors.New("native config failed")
			}
			executor.responses["config"] = response
			report, err := NewDockerRunner(executor).Preflight(context.Background(), request)
			if err != nil || report.OK == failure {
				t.Fatalf("report=%#v error=%v", report, err)
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"process-secret", "file-secret", "configured-secret", "DO_NOT_PRINT_EFFECTIVE_MODEL", "spoofed-env-revision", "spoofed-process-revision"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("report leaked %q: %s", secret, encoded)
				}
			}
			if !strings.Contains(string(encoded), "redacted") || executor.commands[2].Env[GitSHAEnv] != strings.Repeat("a", 40) {
				t.Fatalf("redaction or verified Git metadata missing: %s", encoded)
			}
		})
	}
}

func TestPreflightUnavailableGitNeverUsesEnvironmentRevision(t *testing.T) {
	request, executor := preflightFixture(t, "local")
	executor.responses["git rev-parse --verify HEAD^{commit}"] = preflightResponse{stdout: "spoofed", err: errors.New("not a git repository")}
	writeFile(t, filepath.Join(request.ProjectDir, ".env"), "SSHDOCK_GIT_SHA=spoofed\n")
	t.Setenv(GitSHAEnv, "spoofed")
	report, err := NewDockerRunner(executor).Preflight(context.Background(), request)
	if err != nil || !report.OK || executor.commands[2].Env[GitSHAEnv] != "" {
		t.Fatalf("report=%#v commands=%#v error=%v", report, executor.commands, err)
	}
	findPreflightCheck(t, report, "commit metadata", "unknown")
}

func TestPreflightGitProbeIgnoresInheritedRepositorySelection(t *testing.T) {
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_CONFIG_COUNT"} {
		t.Setenv(name, "unrelated-repository")
	}
	request, executor := preflightFixture(t, "local")
	if _, err := NewDockerRunner(executor).Preflight(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	probe := executor.commands[0]
	if probe.Name != "env" || probe.Dir != request.ProjectDir {
		t.Fatalf("Git probe = %#v", probe)
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, "GIT_") {
			continue
		}
		found := false
		for index := 0; index+1 < len(probe.Args); index += 2 {
			if probe.Args[index] == "-u" && probe.Args[index+1] == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Git probe inherited %s", name)
		}
	}
}

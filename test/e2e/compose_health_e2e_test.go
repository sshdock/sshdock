//go:build e2e

package e2e

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestDockerRunnerComposeHealthSemanticsEndToEnd(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run Docker Compose health tests")
	}
	requireDocker(t)

	tests := []struct {
		name        string
		serviceYAML string
		timeout     time.Duration
		wantFailure bool
	}{
		{
			name: "healthy service",
			serviceYAML: `image: busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
    command: ["sh", "-c", "while true; do sleep 1; done"]
    healthcheck:
      test: ["CMD", "true"]
      interval: 1s
      timeout: 1s
      retries: 2`,
		},
		{
			name: "running service without healthcheck",
			serviceYAML: `image: busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
    command: ["sh", "-c", "while true; do sleep 1; done"]`,
		},
		{
			name: "unhealthy service",
			serviceYAML: `image: busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
    command: ["sh", "-c", "while true; do sleep 1; done"]
    healthcheck:
      test: ["CMD", "false"]
      interval: 1s
      timeout: 1s
      retries: 1`,
			wantFailure: true,
		},
		{
			name: "service exits immediately",
			serviceYAML: `image: busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
    command: ["true"]`,
			wantFailure: true,
		},
		{
			name: "host deadline bounds health wait",
			serviceYAML: `image: busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
    command: ["sh", "-c", "while true; do sleep 1; done"]
    healthcheck:
      test: ["CMD", "sh", "-c", "sleep 30"]
      interval: 1s
      timeout: 20s
      retries: 5`,
			timeout:     10 * time.Second,
			wantFailure: true,
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projectDir := t.TempDir()
			composePath := filepath.Join(projectDir, "compose.yml")
			if err := os.WriteFile(composePath, []byte("services:\n  web:\n    "+test.serviceYAML+"\n"), 0o644); err != nil {
				t.Fatalf("WriteFile compose: %v", err)
			}
			appName := "health-e2e-" + string(rune('a'+index))
			projectName := compose.ProjectName(appName)
			t.Cleanup(func() {
				_ = runCommandNoFail(projectDir, nil, "docker", "compose", "-f", composePath, "-p", projectName, "down", "-v", "--remove-orphans")
			})

			ctx := context.Background()
			if test.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, test.timeout)
				defer cancel()
			}

			_, err := compose.NewDockerRunner(compose.LocalCommandExecutor{}).Deploy(ctx, compose.DeployRequest{
				AppName:     appName,
				ProjectDir:  projectDir,
				ComposePath: composePath,
			})
			if !test.wantFailure && err != nil {
				t.Fatalf("Deploy: %v", err)
			}
			if !test.wantFailure {
				return
			}
			if err == nil {
				t.Fatal("Deploy error = nil, want health wait failure")
			}
			var deployErr *compose.DeployError
			if !errors.As(err, &deployErr) || deployErr.Stage != compose.DeployStageWaitServices {
				t.Fatalf("Deploy error = %T %[1]v, want %q stage", err, compose.DeployStageWaitServices)
			}
		})
	}
}

func TestPublicExamplesEffectiveRouteEndToEnd(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run effective Compose example tests")
	}
	requireDocker(t)

	tests := []struct {
		name        string
		appName     string
		directory   string
		env         map[string]string
		wantService string
		wantPort    int
	}{
		{name: "static site", directory: "static-site", wantService: "web", wantPort: 18080},
		{name: "wordpress lite", directory: "wordpress-lite", wantService: "web", wantPort: 18081},
		{name: "build service", directory: "build-service", wantService: "web", wantPort: 18083},
		{name: "config app", directory: "config-app", env: map[string]string{"APP_MESSAGE": "example route test"}, wantService: "web", wantPort: 18082},
		{name: "Next.js", appName: "example-nextjs", directory: filepath.Join("frameworks", "nextjs"), wantService: "web", wantPort: 18100},
		{name: "NestJS", appName: "example-nestjs", directory: filepath.Join("frameworks", "nestjs"), wantService: "web", wantPort: 18101},
		{name: "Laravel", appName: "example-laravel", directory: filepath.Join("frameworks", "laravel"), env: map[string]string{"APP_KEY": "public-example-route-key"}, wantService: "web", wantPort: 18102},
		{name: "Gin", appName: "example-gin", directory: filepath.Join("frameworks", "gin"), wantService: "web", wantPort: 18103},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projectDir, err := filepath.Abs(filepath.Join("..", "..", "examples", test.directory))
			if err != nil {
				t.Fatalf("Abs example directory: %v", err)
			}
			appName := test.appName
			if appName == "" {
				appName = "example-" + test.directory
			}
			result, err := compose.NewDockerRunner(effectiveConfigOnlyExecutor{}).Deploy(context.Background(), compose.DeployRequest{
				AppName:     appName,
				ProjectDir:  projectDir,
				ComposePath: filepath.Join(projectDir, "compose.yml"),
				Env:         test.env,
			})
			if err != nil {
				t.Fatalf("Deploy effective-model seam: %v", err)
			}
			if !result.RouteFound || result.RouteTarget != (compose.RouteTarget{ServiceName: test.wantService, Port: test.wantPort}) {
				t.Fatalf("route result = %#v, want %s:%d", result, test.wantService, test.wantPort)
			}
		})
	}
}

func TestFrameworkQuickstartsDockerEndToEnd(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run the framework quickstart Docker tests")
	}
	requireDocker(t)

	tests := []struct {
		name         string
		directory    string
		projectName  string
		url          string
		wantBody     []string
		env          map[string]string
		runtimeCheck string
	}{
		{
			name:        "Next.js",
			directory:   "nextjs",
			projectName: "nextjs-public-example-e2e",
			url:         "http://127.0.0.1:18100",
			wantBody:    []string{"To get started, edit the page.tsx file.", "Deploy Now"},
		},
		{
			name:         "NestJS",
			directory:    "nestjs",
			projectName:  "nestjs-public-example-e2e",
			url:          "http://127.0.0.1:18101",
			wantBody:     []string{"Hello World!"},
			runtimeCheck: "test ! -e /app/src && test ! -e /app/test && test ! -e /app/package.json && test ! -e /app/package-lock.json && test ! -e /app/nest-cli.json && test ! -e /app/tsconfig.json && test ! -e /app/node_modules/@nestjs/cli",
		},
		{
			name:         "Laravel",
			directory:    "laravel",
			projectName:  "laravel-public-example-e2e",
			url:          "http://127.0.0.1:18102",
			wantBody:     []string{"Documentation", "Deploy now"},
			env:          map[string]string{"APP_KEY": "base64:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
			runtimeCheck: `test -z "$(find /app -name .gitignore -print -quit)" && test ! -e /app/storage/framework/testing && test ! -e /app/composer.json && test ! -e /app/package.json && test ! -e /app/tests && test ! -e /app/vendor/bin/phpunit`,
		},
		{
			name:         "Gin",
			directory:    "gin",
			projectName:  "gin-public-example-e2e",
			url:          "http://127.0.0.1:18103/ping",
			wantBody:     []string{"pong"},
			runtimeCheck: `test ! -e /workspace && test ! -e /usr/local/go && ! command -v git && ! command -v go && test "$(id -u)" = 65532`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for key, value := range test.env {
				t.Setenv(key, value)
			}
			// Given a registered framework quickstart and a clean Compose project.
			projectDir, err := filepath.Abs(filepath.Join("..", "..", "examples", "frameworks", test.directory))
			if err != nil {
				t.Fatalf("Abs %s example directory: %v", test.name, err)
			}
			projectName := compose.ProjectName(test.projectName)
			t.Cleanup(func() {
				_ = runCommandNoFail(projectDir, nil, "docker", "compose", "-p", projectName, "down", "-v", "--remove-orphans")
			})

			// When Docker Compose builds and waits for the root-page healthcheck.
			runCommand(t, projectDir, nil, "docker", "compose", "-p", projectName, "up", "--build", "--wait")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, test.url, nil)
			if err != nil {
				t.Fatalf("NewRequestWithContext: %v", err)
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("GET %s quickstart: %v", test.name, err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			if err != nil {
				t.Fatalf("read %s quickstart response: %v", test.name, err)
			}

			// Then the production container serves the official starter surface.
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET status = %d, want %d", response.StatusCode, http.StatusOK)
			}
			for _, want := range test.wantBody {
				if !strings.Contains(string(body), want) {
					t.Fatalf("%s response missing %q", test.name, want)
				}
			}
			if test.runtimeCheck != "" {
				runCommand(t, projectDir, nil, "docker", "compose", "-p", projectName, "exec", "-T", "web", "sh", "-c", test.runtimeCheck)
			}
		})
	}
}

type effectiveConfigOnlyExecutor struct{}

func (effectiveConfigOnlyExecutor) Run(ctx context.Context, command compose.Command) (string, error) {
	for _, arg := range command.Args {
		if arg == "config" {
			return (compose.LocalCommandExecutor{}).Run(ctx, command)
		}
	}
	return "", nil
}

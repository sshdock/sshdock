//go:build e2e

package e2e

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

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
		{
			name:         "Phoenix LiveView",
			directory:    "phoenix",
			projectName:  "phoenix-public-example-e2e",
			url:          "http://127.0.0.1:18104/items",
			wantBody:     []string{"Listing Items", "New Item"},
			env:          map[string]string{"SECRET_KEY_BASE": "phoenix-public-example-secret-key-base-must-be-at-least-sixty-four-bytes", "PHX_HOST": "127.0.0.1"},
			runtimeCheck: `test ! -e /workspace && ! command -v mix && ! command -v elixir && ! command -v git && test "$(id -u)" = 65534 && test -x /app/bin/server && test -x /app/bin/migrate`,
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

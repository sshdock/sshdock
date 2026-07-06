package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestExamplesAreRunnableDocsContracts(t *testing.T) {
	root := repoRoot(t)

	tests := []struct {
		name          string
		dir           string
		wantService   string
		wantRoutePort int
		requiredFiles []string
	}{
		{
			name:          "static site",
			dir:           filepath.Join(root, "examples", "static-site"),
			wantService:   "web",
			wantRoutePort: 18080,
			requiredFiles: []string{
				"README.md",
				"compose.yml",
				filepath.Join("public", "index.html"),
			},
		},
		{
			name:          "wordpress lite",
			dir:           filepath.Join(root, "examples", "wordpress-lite"),
			wantService:   "web",
			wantRoutePort: 18081,
			requiredFiles: []string{
				"README.md",
				"compose.yml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, requiredFile := range tt.requiredFiles {
				path := filepath.Join(tt.dir, requiredFile)
				if !fileExists(path) {
					t.Fatalf("expected example file %s to exist", path)
				}
			}

			composePath := filepath.Join(tt.dir, "compose.yml")
			if _, err := compose.ValidateFile(composePath); err != nil {
				t.Fatalf("ValidateFile(%s): %v", composePath, err)
			}

			target, ok, reason, err := compose.InferDefaultRoute(composePath)
			if err != nil {
				t.Fatalf("InferDefaultRoute(%s): %v", composePath, err)
			}
			if !ok {
				t.Fatalf("InferDefaultRoute(%s) ok = false, reason = %q", composePath, reason)
			}
			if target.ServiceName != tt.wantService || target.Port != tt.wantRoutePort {
				t.Fatalf("route target = %#v, want %s:%d", target, tt.wantService, tt.wantRoutePort)
			}
		})
	}
}

func TestWordPressExampleWaitsForDatabaseReadiness(t *testing.T) {
	root := repoRoot(t)
	composePath := filepath.Join(root, "examples", "wordpress-lite", "compose.yml")
	contents, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", composePath, err)
	}

	text := string(contents)
	for _, want := range []string{
		"condition: service_healthy",
		"healthcheck:",
		"mariadb-admin ping",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("wordpress-lite compose file missing %q", want)
		}
	}
}

func TestExamplesDocumentCleanup(t *testing.T) {
	root := repoRoot(t)

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "public examples guide",
			path: filepath.Join(root, "docs", "EXAMPLES.md"),
			want: []string{
				"Clean up:",
				"sudo sshdock apps remove static-site --force",
				"sudo sshdock apps remove wordpress-lite --force",
				"sudo docker volume rm sshdock_wordpress-lite_wordpress-data sshdock_wordpress-lite_mariadb-data",
			},
		},
		{
			name: "static site readme",
			path: filepath.Join(root, "examples", "static-site", "README.md"),
			want: []string{
				"## Clean Up",
				"sudo sshdock apps remove static-site --force",
				"No Docker volumes need to be removed",
			},
		},
		{
			name: "wordpress lite readme",
			path: filepath.Join(root, "examples", "wordpress-lite", "README.md"),
			want: []string{
				"## Clean Up",
				"sudo sshdock apps remove wordpress-lite --force",
				"sudo docker volume rm sshdock_wordpress-lite_wordpress-data sshdock_wordpress-lite_mariadb-data",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contents, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", tt.path, err)
			}
			text := string(contents)
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Fatalf("%s missing %q", tt.path, want)
				}
			}
		})
	}
}

func TestExamplesDocumentGitHubCopy(t *testing.T) {
	root := repoRoot(t)

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "public examples guide",
			path: filepath.Join(root, "docs", "EXAMPLES.md"),
			want: []string{
				"mkdir static-site",
				"mkdir wordpress-lite",
				"raw.githubusercontent.com/sshdock/sshdock/main/examples/static-site",
				"raw.githubusercontent.com/sshdock/sshdock/main/examples/wordpress-lite",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/static-site/compose.yml",
				"curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/main/examples/static-site/public/index.html",
				"git init -b main",
			},
		},
		{
			name: "static site readme",
			path: filepath.Join(root, "examples", "static-site", "README.md"),
			want: []string{
				"mkdir static-site",
				"raw.githubusercontent.com/sshdock/sshdock/main/examples/static-site",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/static-site/compose.yml",
				"curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/main/examples/static-site/public/index.html",
				"git init -b main",
			},
		},
		{
			name: "wordpress lite readme",
			path: filepath.Join(root, "examples", "wordpress-lite", "README.md"),
			want: []string{
				"mkdir wordpress-lite",
				"raw.githubusercontent.com/sshdock/sshdock/main/examples/wordpress-lite",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/wordpress-lite/compose.yml",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/main/examples/wordpress-lite/README.md",
				"git init -b main",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contents, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", tt.path, err)
			}
			text := string(contents)
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Fatalf("%s missing %q", tt.path, want)
				}
			}
			if strings.Contains(text, "cp -R examples/") {
				t.Fatalf("%s should document GitHub copy, not local cp -R", tt.path)
			}
			for _, avoid := range []string{"SSHD_REF", "SSHD_RAW", "/tmp/sshdock-examples"} {
				if strings.Contains(text, avoid) {
					t.Fatalf("%s should not contain %q", tt.path, avoid)
				}
			}
		})
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

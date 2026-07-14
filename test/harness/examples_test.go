package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestExamplesAreRunnableDocsContracts(t *testing.T) {
	root := repoRoot(t)

	tests := []struct {
		name          string
		dir           string
		wantConfig    []appconfig.RequiredKey
		requiredFiles []string
	}{
		{
			name: "static site",
			dir:  filepath.Join(root, "examples", "static-site"),
			requiredFiles: []string{
				"README.md",
				"compose.yml",
				filepath.Join("public", "index.html"),
			},
		},
		{
			name: "wordpress lite",
			dir:  filepath.Join(root, "examples", "wordpress-lite"),
			requiredFiles: []string{
				"README.md",
				"compose.yml",
			},
		},
		{
			name: "build service",
			dir:  filepath.Join(root, "examples", "build-service"),
			requiredFiles: []string{
				"README.md",
				"compose.yml",
				"Dockerfile",
				"server.py",
			},
		},
		{
			name: "config app",
			dir:  filepath.Join(root, "examples", "config-app"),
			wantConfig: []appconfig.RequiredKey{
				{Name: "APP_MESSAGE"},
			},
			requiredFiles: []string{
				"README.md",
				"compose.yml",
				"Dockerfile",
				"server.py",
				".sshdock.yml",
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

			if len(tt.wantConfig) > 0 {
				manifest, err := appconfig.LoadManifest(tt.dir)
				if err != nil {
					t.Fatalf("LoadManifest(%s): %v", tt.dir, err)
				}
				if len(manifest.Required) != len(tt.wantConfig) {
					t.Fatalf("manifest required keys = %#v, want %#v", manifest.Required, tt.wantConfig)
				}
				for i, want := range tt.wantConfig {
					if manifest.Required[i] != want {
						t.Fatalf("manifest required key %d = %#v, want %#v", i, manifest.Required[i], want)
					}
				}
			}
		})
	}
}

func TestConfigExampleDocumentsConfigWorkflow(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "config-app")

	if fileExists(filepath.Join(dir, ".env")) {
		t.Fatalf("config-app must not commit .env")
	}

	composePath := filepath.Join(dir, "compose.yml")
	composeContents, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", composePath, err)
	}
	if !strings.Contains(string(composeContents), "APP_MESSAGE=${APP_MESSAGE}") {
		t.Fatalf("compose file must pass APP_MESSAGE from SSHDock config")
	}

	readmePath := filepath.Join(dir, "README.md")
	readmeContents, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", readmePath, err)
	}
	readme := string(readmeContents)
	for _, want := range []string{
		"git push sshdock main",
		"missing required config",
		"ssh dashboard@sshdock.example.com config set config-app APP_MESSAGE",
		"ssh dashboard@sshdock.example.com config list config-app",
		"ssh dashboard@sshdock.example.com config get config-app APP_MESSAGE",
		"curl -fsS https://config-app.example.com",
		"SSHDock config example:",
		"push again",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("%s missing %q", readmePath, want)
		}
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

func TestRollbackLabDocumentsPostReceiveDeployFailure(t *testing.T) {
	root := repoRoot(t)

	tests := []struct {
		name string
		path string
	}{
		{
			name: "public examples guide",
			path: filepath.Join(root, "docs", "EXAMPLES.md"),
		},
		{
			name: "rollback lab readme",
			path: filepath.Join(root, "examples", "rollback-lab", "README.md"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contents, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", tt.path, err)
			}
			text := string(contents)
			if strings.Contains(text, "The push fails during deploy") {
				t.Fatalf("%s should describe deploy failure, not a failed git push", tt.path)
			}
			for _, want := range []string{
				"The Git push may complete because SSHDock deploys from a post-receive hook.",
				"`deploy.failed`",
			} {
				if !strings.Contains(text, want) {
					t.Fatalf("%s missing %q", tt.path, want)
				}
			}
		})
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
				"sudo sshdock apps remove build-service --force",
				"sudo sshdock apps remove config-app --force",
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
		{
			name: "build service readme",
			path: filepath.Join(root, "examples", "build-service", "README.md"),
			want: []string{
				"## Clean Up",
				"sudo sshdock apps remove build-service --force",
				"No Docker volumes need to be removed",
			},
		},
		{
			name: "config app readme",
			path: filepath.Join(root, "examples", "config-app", "README.md"),
			want: []string{
				"## Clean Up",
				"sudo sshdock apps remove config-app --force",
				"No Docker volumes need to be removed",
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
				"mkdir build-service",
				"mkdir config-app",
				"raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site",
				"raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/wordpress-lite",
				"raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service",
				"raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site/compose.yml",
				"curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site/public/index.html",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/Dockerfile",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/server.py",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/.sshdock.yml",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/server.py",
				"git init -b main",
			},
		},
		{
			name: "static site readme",
			path: filepath.Join(root, "examples", "static-site", "README.md"),
			want: []string{
				"mkdir static-site",
				"raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site/compose.yml",
				"curl -fsSLo public/index.html https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/static-site/public/index.html",
				"git init -b main",
			},
		},
		{
			name: "wordpress lite readme",
			path: filepath.Join(root, "examples", "wordpress-lite", "README.md"),
			want: []string{
				"mkdir wordpress-lite",
				"raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/wordpress-lite",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/wordpress-lite/compose.yml",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/wordpress-lite/README.md",
				"git init -b main",
			},
		},
		{
			name: "build service readme",
			path: filepath.Join(root, "examples", "build-service", "README.md"),
			want: []string{
				"mkdir build-service",
				"raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/compose.yml",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/Dockerfile",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/server.py",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/build-service/README.md",
				"git init -b main",
			},
		},
		{
			name: "config app readme",
			path: filepath.Join(root, "examples", "config-app", "README.md"),
			want: []string{
				"mkdir config-app",
				"raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/compose.yml",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/.sshdock.yml",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/Dockerfile",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/server.py",
				"curl -fsSLO https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/examples/config-app/README.md",
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

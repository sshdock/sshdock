package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdoptionDocsExistAndKeepV0Boundaries(t *testing.T) {
	root := repoRoot(t)
	docs := []struct {
		path   string
		want   []string
		reject []string
	}{
		{
			path: filepath.Join(root, "docs", "COMPARE_DOKKU.md"),
			want: []string{
				"SSHDock vs Dokku",
				"Compose-first",
				"not a drop-in Dokku replacement",
				"MIGRATE_FROM_DOKKU.md",
			},
		},
		{
			path: filepath.Join(root, "docs", "COMPARE_DOKPLOY.md"),
			want: []string{
				"SSHDock vs Dokploy",
				"not a claim that SSHDock replaces Dokploy",
				"No web dashboard",
				"MIGRATE_FROM_DOKPLOY.md",
			},
		},
		{
			path: filepath.Join(root, "docs", "MIGRATE_FROM_DOKKU.md"),
			want: []string{
				"does not run Procfiles, buildpacks, Dokku plugins",
				"compose.yaml",
				"docker-compose.yaml",
				"COMPOSE_SUPPORT.md",
				"config set my-app DATABASE_URL",
				"backup create",
			},
		},
		{
			path: filepath.Join(root, "docs", "MIGRATE_FROM_DOKPLOY.md"),
			want: []string{
				"does not provide a web dashboard",
				"Docker Stack",
				"config import my-app",
				"domains check my-app",
			},
		},
		{
			path: filepath.Join(root, "docs", "TROUBLESHOOTING.md"),
			want: []string{
				"sudo sshdock diagnostics",
				"stage=...",
				"domains check",
				"Do not append a remote `operator` command.",
			},
			reject: []string{"Unsupported Compose field"},
		},
	}

	for _, doc := range docs {
		t.Run(filepath.Base(doc.path), func(t *testing.T) {
			contents, err := os.ReadFile(doc.path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", doc.path, err)
			}
			text := string(contents)
			for _, want := range doc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("%s missing %q", doc.path, want)
				}
			}
			for _, reject := range doc.reject {
				if strings.Contains(text, reject) {
					t.Fatalf("%s contains obsolete guidance %q", doc.path, reject)
				}
			}
		})
	}
}

func TestBreakingComposeFirstDocsRejectRemovedBehaviorAndStateHostBoundary(t *testing.T) {
	// Given the public docs and example guides that describe the shipped operator contract.
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "docs", "INSTALL.md"),
		filepath.Join(root, "docs", "CLI_COMMANDS.md"),
		filepath.Join(root, "docs", "COMPOSE_SUPPORT.md"),
		filepath.Join(root, "docs", "EXAMPLES.md"),
		filepath.Join(root, "docs", "MIGRATE_FROM_DOKKU.md"),
		filepath.Join(root, "docs", "MIGRATE_FROM_DOKPLOY.md"),
		filepath.Join(root, "docs", "TROUBLESHOOTING.md"),
	}
	examplePaths, err := filepath.Glob(filepath.Join(root, "examples", "*", "README.md"))
	if err != nil {
		t.Fatalf("glob example docs: %v", err)
	}
	paths = append(paths, examplePaths...)

	// When each public surface is inspected for behavior removed by the breaking contract.
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		text := string(contents)

		// Then no current instructions expose the removed control-plane or dashboard account.
		for _, reject := range []string{
			"`apps rollback`",
			".sshdock.yml",
			"--scope",
			"dashboard@",
			"through a `dashboard` user",
			"/var/lib/sshdock/dashboard/",
			"\n  dashboard/\n",
		} {
			if strings.Contains(text, reject) {
				t.Fatalf("%s contains removed contract %q", path, reject)
			}
		}
	}

	// Then installation guidance assigns host operations to normal server tooling.
	installContents, err := os.ReadFile(filepath.Join(root, "docs", "INSTALL.md"))
	if err != nil {
		t.Fatalf("ReadFile(INSTALL.md): %v", err)
	}
	for _, want := range []string{
		"operating-system patching",
		"firewall policy",
		"disk monitoring",
		"Docker Engine maintenance",
		"Caddy maintenance",
		"VPS-provider operations",
		"third-party monitoring",
	} {
		if !strings.Contains(string(installContents), want) {
			t.Fatalf("INSTALL.md missing host ownership boundary %q", want)
		}
	}
}

package harness

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

type publicExampleContract struct {
	name          string
	category      string
	guidePath     string
	path          string
	requiredFiles []string
	exactFiles    []string
}

func TestPublicExamples_contract_when_example_is_registered(t *testing.T) {
	// Given the public example registry and its shared documentation contract.
	t.Setenv("APP_KEY", "public-example-contract-key")
	t.Setenv("SECRET_KEY_BASE", "phoenix-public-example-secret-key-base-must-be-at-least-sixty-four-bytes")
	t.Setenv("PHX_HOST", "phoenix.example.com")
	t.Setenv("WORDPRESS_DB_NAME", "wordpress")
	t.Setenv("WORDPRESS_DB_USER", "wordpress")
	t.Setenv("WORDPRESS_DB_PASSWORD", "public-example-contract-password")
	t.Setenv("WORDPRESS_DB_ROOT_PASSWORD", "public-example-contract-root-password")
	root := repoRoot(t)
	examples := []publicExampleContract{
		{
			name:      "Next.js",
			category:  "Framework quickstarts",
			guidePath: "examples/frameworks/nextjs",
			path:      filepath.Join(root, "examples", "frameworks", "nextjs"),
			exactFiles: []string{
				"Dockerfile",
				"README.md",
				"compose.yml",
			},
		},
		{
			name:      "NestJS",
			category:  "Framework quickstarts",
			guidePath: "examples/frameworks/nestjs",
			path:      filepath.Join(root, "examples", "frameworks", "nestjs"),
			exactFiles: []string{
				"Dockerfile",
				"README.md",
				"compose.yml",
			},
		},
		{
			name:      "Laravel",
			category:  "Framework quickstarts",
			guidePath: "examples/frameworks/laravel",
			path:      filepath.Join(root, "examples", "frameworks", "laravel"),
			exactFiles: []string{
				"Dockerfile",
				"README.md",
				"compose.yml",
			},
		},
		{
			name:      "Gin",
			category:  "Framework quickstarts",
			guidePath: "examples/frameworks/gin",
			path:      filepath.Join(root, "examples", "frameworks", "gin"),
			exactFiles: []string{
				"Dockerfile",
				"README.md",
				"compose.yml",
			},
		},
		{
			name:      "Phoenix LiveView",
			category:  "Framework quickstarts",
			guidePath: "examples/frameworks/phoenix",
			path:      filepath.Join(root, "examples", "frameworks", "phoenix"),
			exactFiles: []string{
				"Dockerfile",
				"README.md",
				"compose.yml",
			},
		},
		{
			name:      "WordPress",
			category:  "Software recipes",
			guidePath: "examples/software/wordpress",
			path:      filepath.Join(root, "examples", "software", "wordpress"),
			exactFiles: []string{
				"README.md",
				"compose.yml",
			},
		},
	}
	requiredSections := []string{
		"Purpose",
		"Prerequisites",
		"Topology",
		"Pinned versions",
		"Deploy",
		"Verify",
		"Operate",
		"Upgrade",
		"Cleanup",
		"Persistence",
		"Limitations",
		"Security boundaries",
	}

	// When each registered example is inspected through its public files.
	for _, example := range examples {
		t.Run(example.name, func(t *testing.T) {
			for _, requiredFile := range example.requiredFiles {
				path := filepath.Join(example.path, requiredFile)
				if !fileExists(path) {
					t.Fatalf("required file %s does not exist", path)
				}
			}
			if example.exactFiles != nil {
				files := repositoryFiles(t, example.path)
				if !slices.Equal(files, example.exactFiles) {
					t.Fatalf("repository files = %v, want %v", files, example.exactFiles)
				}
			}
			composePath, err := compose.DetectFile(example.path)
			if err != nil {
				t.Fatalf("DetectFile(%s): %v", example.path, err)
			}
			if _, err := compose.ValidateFile(composePath); err != nil {
				t.Fatalf("ValidateFile(%s): %v", composePath, err)
			}

			readme := readTextFile(t, filepath.Join(example.path, "README.md"))
			for _, section := range requiredSections {
				if !strings.Contains(readme, "## "+section) {
					t.Fatalf("README missing %q section", section)
				}
			}
		})
	}

	// Then the canonical guide distinguishes every public category and registers each example.
	guide := readTextFile(t, filepath.Join(root, "docs", "EXAMPLES.md"))
	for _, category := range []string{
		"Framework quickstarts",
		"Software recipes",
		"Database examples",
		"Feature labs",
	} {
		if !strings.Contains(guide, "## "+category) {
			t.Fatalf("EXAMPLES.md missing %q category", category)
		}
	}
	for _, example := range examples {
		if !strings.Contains(guide, example.category) || !strings.Contains(guide, example.guidePath) {
			t.Fatalf("EXAMPLES.md does not register %s under %s", example.name, example.category)
		}
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(contents)
}

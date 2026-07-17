package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
}

func TestPublicExamples_contract_when_example_is_registered(t *testing.T) {
	// Given the public example registry and its shared documentation contract.
	root := repoRoot(t)
	examples := []publicExampleContract{
		{
			name:      "Next.js",
			category:  "Framework quickstarts",
			guidePath: "examples/frameworks/nextjs",
			path:      filepath.Join(root, "examples", "frameworks", "nextjs"),
			requiredFiles: []string{
				".dockerignore",
				".gitignore",
				"Dockerfile",
				"README.md",
				"compose.yml",
				"next.config.ts",
				"package-lock.json",
				"package.json",
				"postcss.config.js",
				"tsconfig.json",
				filepath.Join("public", "next.svg"),
				filepath.Join("app", "globals.css"),
				filepath.Join("app", "layout.tsx"),
				filepath.Join("app", "page.tsx"),
			},
		},
		{
			name:      "NestJS",
			category:  "Framework quickstarts",
			guidePath: "examples/frameworks/nestjs",
			path:      filepath.Join(root, "examples", "frameworks", "nestjs"),
			requiredFiles: []string{
				".dockerignore",
				".gitignore",
				".prettierrc",
				"Dockerfile",
				"README.md",
				"compose.yml",
				"eslint.config.mjs",
				"nest-cli.json",
				"package-lock.json",
				"package.json",
				"tsconfig.build.json",
				"tsconfig.json",
				filepath.Join("src", "app.controller.spec.ts"),
				filepath.Join("src", "app.controller.ts"),
				filepath.Join("src", "app.module.ts"),
				filepath.Join("src", "app.service.ts"),
				filepath.Join("src", "main.ts"),
				filepath.Join("test", "app.e2e-spec.ts"),
				filepath.Join("test", "jest-e2e.json"),
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

func TestNextJSQuickstart_contract_when_built_for_production(t *testing.T) {
	// Given the registered Next.js quickstart.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "frameworks", "nextjs")

	// When its dependency, image, and Compose contracts are inspected.
	packageJSON := readPackageJSON(t, filepath.Join(dir, "package.json"))
	for group, dependencies := range map[string]map[string]string{
		"dependencies":    packageJSON.Dependencies,
		"devDependencies": packageJSON.DevDependencies,
		"overrides":       packageJSON.Overrides,
	} {
		for name, version := range dependencies {
			if !exactVersionPattern.MatchString(version) {
				t.Fatalf("%s %s version %q is not pinned exactly", group, name, version)
			}
		}
	}

	dockerfile := readTextFile(t, filepath.Join(dir, "Dockerfile"))
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	for _, want := range []string{"npm ci", "npm run build", ".next/standalone", "CMD [\"node\", \"server.js\"]"} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing production marker %q", want)
		}
	}
	if strings.Contains(dockerfile, "next dev") || strings.Contains(composeFile, "next dev") {
		t.Fatal("production quickstart must not run the Next.js development server")
	}
	for _, want := range []string{"127.0.0.1:18100:3000", "healthcheck:", "restart: unless-stopped"} {
		if !strings.Contains(composeFile, want) {
			t.Fatalf("compose.yml missing production marker %q", want)
		}
	}

	// Then the documented workflow covers the complete user-visible lifecycle.
	readme := readTextFile(t, filepath.Join(dir, "README.md"))
	for _, want := range []string{
		"git push sshdock main",
		"curl -fsS https://nextjs.example.com",
		"sshdock apps health nextjs",
		"sshdock logs nextjs web",
		"sshdock apps restart nextjs",
		"npm install --save-exact",
		"sshdock apps remove nextjs --force",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing workflow command %q", want)
		}
	}
}

var exactVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`)

func TestNestJSQuickstart_contract_when_built_for_production(t *testing.T) {
	// Given the official Nest CLI starter with its additive SSHDock envelope.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "frameworks", "nestjs")

	// When its image, runtime, and Compose contracts are inspected.
	dockerfile := readTextFile(t, filepath.Join(dir, "Dockerfile"))
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	for _, want := range []string{
		"FROM node:24.18.0-alpine3.24 AS build",
		"FROM node:24.18.0-alpine3.24 AS runtime",
		"npm ci",
		"npm run build",
		"npm ci --omit=dev",
		"USER node",
		"CMD [\"node\", \"dist/main\"]",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing production marker %q", want)
		}
	}
	if strings.Contains(dockerfile, "start:dev") || strings.Contains(composeFile, "start:dev") {
		t.Fatal("production quickstart must not run the NestJS development server")
	}
	for _, want := range []string{"127.0.0.1:18101:3000", "healthcheck:", "restart: unless-stopped"} {
		if !strings.Contains(composeFile, want) {
			t.Fatalf("compose.yml missing production marker %q", want)
		}
	}

	// Then its public workflow records provenance and covers the complete lifecycle.
	readme := readTextFile(t, filepath.Join(dir, "README.md"))
	for _, want := range []string{
		"@nestjs/cli@11.0.24 new nestjs --package-manager npm --strict",
		"git push sshdock main",
		"curl -fsS https://nestjs.example.com",
		"sshdock apps health nestjs",
		"sshdock logs nestjs web",
		"sshdock apps restart nestjs",
		"sshdock apps redeploy nestjs",
		"npm run test:e2e",
		"sshdock apps remove nestjs --force",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing workflow command %q", want)
		}
	}
}

type packageManifest struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Overrides       map[string]string `json:"overrides"`
}

func readPackageJSON(t *testing.T, path string) packageManifest {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	var manifest packageManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return manifest
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(contents)
}

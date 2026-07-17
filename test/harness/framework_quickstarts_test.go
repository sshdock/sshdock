package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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

func TestLaravelQuickstart_contract_when_built_for_production(t *testing.T) {
	// Given the official Laravel application skeleton with its additive SSHDock envelope.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "frameworks", "laravel")

	// When its image, runtime, config, persistence, and Compose contracts are inspected.
	dockerfile := readTextFile(t, filepath.Join(dir, "Dockerfile"))
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	for _, want := range []string{
		"FROM node:24.18.0-alpine3.24 AS assets",
		"FROM dunglas/frankenphp:1.12.3-php8.5-alpine AS base",
		"COPY --from=composer:2.10.2 /usr/bin/composer /usr/bin/composer",
		"npm ci",
		"npm run build",
		"composer install --no-dev",
		"php.ini-production",
		"USER www-data",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing production marker %q", want)
		}
	}
	if strings.Contains(dockerfile, "artisan serve") || strings.Contains(composeFile, "artisan serve") {
		t.Fatal("production quickstart must not run Laravel's development server")
	}
	for _, want := range []string{
		"127.0.0.1:18102:8080",
		"APP_KEY: ${APP_KEY:?set APP_KEY with sshdock config set}",
		"ASSET_URL: ${APP_URL:-https://laravel.example.com}",
		"laravel_storage:/app/storage",
		"healthcheck:",
		"restart: unless-stopped",
	} {
		if !strings.Contains(composeFile, want) {
			t.Fatalf("compose.yml missing production marker %q", want)
		}
	}

	// Then its public workflow records exact provenance and covers the complete lifecycle.
	readme := readTextFile(t, filepath.Join(dir, "README.md"))
	for _, want := range []string{
		"laravel/laravel:v13.8.0",
		"e196bfdfc96903f2e10219749fcbca7c0aefe99f",
		"git push sshdock main",
		"config set laravel APP_KEY",
		"domains attach laravel web laravel.example.com --port 18102",
		"curl -fsS https://laravel.example.com",
		"sshdock apps health laravel",
		"sshdock logs laravel web",
		"sshdock apps restart laravel",
		"sshdock apps redeploy laravel",
		"apps exec laravel web -- php artisan about",
		"apps run laravel web -- php artisan migrate --force",
		"sshdock apps remove laravel --force",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing workflow or provenance marker %q", want)
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

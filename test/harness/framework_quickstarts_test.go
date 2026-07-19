package harness

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNextJSCompatibilityProbe_contract_when_generated_for_production(t *testing.T) {
	// Given the registered Next.js compatibility probe.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "frameworks", "nextjs")

	// When its generator, image, production, and Compose contracts are inspected.
	dockerfile := readTextFile(t, filepath.Join(dir, "Dockerfile"))
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	for _, want := range []string{
		"ARG CREATE_NEXT_APP_VERSION=16.2.10",
		"ARG NODE_IMAGE=node:24.13.0-slim@sha256:4660b1ca8b28d6d1906fd644abe34b2ed81d15434d26d845ef0aced307cf4b6f",
		"FROM ${NODE_IMAGE} AS source",
		"npx --yes create-next-app@${CREATE_NEXT_APP_VERSION} . --yes --use-npm --disable-git",
		"FROM ${NODE_IMAGE} AS build",
		"npm run build",
		"npm prune --omit=dev",
		"FROM build AS runtime-output",
		"rm -rf .next/cache .next/diagnostics .next/types",
		"FROM ${NODE_IMAGE} AS runtime",
		"COPY --from=runtime-output --chown=node:node /app/.next ./.next",
		"COPY --from=runtime-output --chown=node:node /app/node_modules ./node_modules",
		"USER node",
		"CMD [\"npm\", \"start\"]",
	} {
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
		"create-next-app@16.2.10",
		"node:24.13.0-slim@sha256:4660b1ca8b28d6d1906fd644abe34b2ed81d15434d26d845ef0aced307cf4b6f",
		"npx create-next-app@latest my-app --yes",
		"https://nextjs.org/docs/app/getting-started/installation",
		"git push sshdock main",
		"curl -fsS https://nextjs.example.com",
		"sshdock apps health nextjs",
		"sshdock logs nextjs web",
		"sshdock apps restart nextjs",
		"sshdock apps redeploy nextjs",
		"sshdock apps remove nextjs --force",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing workflow command %q", want)
		}
	}
}

func TestNestJSCompatibilityProbe_contract_when_generated_for_production(t *testing.T) {
	// Given the registered NestJS compatibility probe.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "frameworks", "nestjs")

	// When its generator, image, runtime, and Compose contracts are inspected.
	dockerfile := readTextFile(t, filepath.Join(dir, "Dockerfile"))
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	for _, want := range []string{
		"ARG NEST_CLI_VERSION=11.0.24",
		"ARG NODE_IMAGE=node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd",
		"FROM ${NODE_IMAGE} AS source",
		"npx --yes @nestjs/cli@${NEST_CLI_VERSION} new app --package-manager npm --strict --skip-git",
		"FROM source AS build",
		"npm run build",
		"npm prune --omit=dev --no-audit --no-fund",
		"FROM build AS runtime-output",
		"FROM ${NODE_IMAGE} AS runtime",
		"COPY --from=runtime-output --chown=node:node /workspace/app/dist ./dist",
		"COPY --from=runtime-output --chown=node:node /workspace/app/node_modules ./node_modules",
		"USER node",
		"CMD [\"node\", \"dist/main\"]",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing production marker %q", want)
		}
	}
	if strings.Contains(dockerfile, "start:dev") || strings.Contains(composeFile, "start:dev") {
		t.Fatal("production compatibility probe must not run the NestJS development server")
	}
	for _, want := range []string{"127.0.0.1:18101:3000", "healthcheck:", "restart: unless-stopped"} {
		if !strings.Contains(composeFile, want) {
			t.Fatalf("compose.yml missing production marker %q", want)
		}
	}

	// Then its public workflow records exact provenance and covers the complete lifecycle.
	readme := readTextFile(t, filepath.Join(dir, "README.md"))
	for _, want := range []string{
		"NestJS framework compatibility probe",
		"@nestjs/cli@11.0.24",
		"node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd",
		"https://docs.nestjs.com/first-steps",
		"npx @nestjs/cli@latest new my-app --package-manager npm --strict",
		"git push sshdock main",
		"curl -fsS https://nestjs.example.com",
		"sshdock apps health nestjs",
		"sshdock logs nestjs web",
		"sshdock apps restart nestjs",
		"sshdock apps redeploy nestjs",
		"sshdock apps remove nestjs --force",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing workflow or provenance marker %q", want)
		}
	}
}

func TestLaravelCompatibilityProbe_contract_when_generated_for_production(t *testing.T) {
	// Given the registered Laravel compatibility probe.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "frameworks", "laravel")

	// When its generator, image, runtime, config, persistence, and Compose contracts are inspected.
	dockerfile := readTextFile(t, filepath.Join(dir, "Dockerfile"))
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	for _, want := range []string{
		"ARG LARAVEL_SKELETON_VERSION=13.8.0",
		"ARG COMPOSER_IMAGE=composer:2.10.2@sha256:5946476338742b200bb9ff88f8be56275ddae4b3949c72305cb0dbf10cfcb760",
		"ARG NODE_IMAGE=node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd",
		"ARG FRANKENPHP_IMAGE=dunglas/frankenphp:1.12.3-php8.5-alpine@sha256:19eda5f22186afeda3aaa70f103a7019bbcff57980da8069f7861c1034aa81ae",
		"FROM ${COMPOSER_IMAGE} AS source",
		"composer create-project --no-interaction --prefer-dist --no-install --no-scripts laravel/laravel:${LARAVEL_SKELETON_VERSION} .",
		"FROM ${FRANKENPHP_IMAGE} AS base",
		"FROM base AS dependencies",
		"composer install --no-dev",
		"FROM ${NODE_IMAGE} AS assets",
		"npm install --no-audit --no-fund",
		"npm run build",
		"FROM dependencies AS runtime-output",
		"FROM base AS runtime",
		"COPY --from=runtime-output --chown=www-data:www-data /app ./",
		"php.ini-production",
		"USER www-data",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing production marker %q", want)
		}
	}
	if strings.Contains(dockerfile, "artisan serve") || strings.Contains(composeFile, "artisan serve") {
		t.Fatal("production compatibility probe must not run Laravel's development server")
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
		"Laravel framework compatibility probe",
		"laravel/laravel:13.8.0",
		"composer:2.10.2@sha256:5946476338742b200bb9ff88f8be56275ddae4b3949c72305cb0dbf10cfcb760",
		"node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd",
		"dunglas/frankenphp:1.12.3-php8.5-alpine@sha256:19eda5f22186afeda3aaa70f103a7019bbcff57980da8069f7861c1034aa81ae",
		"https://laravel.com/docs/13.x/installation",
		"composer create-project laravel/laravel example-app",
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

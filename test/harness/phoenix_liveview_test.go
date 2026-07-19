package harness

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPhoenixLiveViewCompatibilityProbe_contract_when_generated_for_production(t *testing.T) {
	// Given the registered Phoenix LiveView compatibility probe.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "frameworks", "phoenix")

	// When its generator, image, release, LiveView, and Compose contracts are inspected.
	dockerfile := readTextFile(t, filepath.Join(dir, "Dockerfile"))
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	for _, want := range []string{
		"ARG PHX_NEW_VERSION=1.8.9",
		"ARG ELIXIR_IMAGE=hexpm/elixir:1.20.2-erlang-28.5.0.3-alpine-3.23.5@sha256:6f03034e254126f063959873d8d3b811ee92abaabab27b62c53982c4a1034e39",
		"ARG ALPINE_IMAGE=alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40",
		"mix archive.install hex phx_new ${PHX_NEW_VERSION} --force",
		"mix phx.new app --database sqlite3 --no-mailer --no-dashboard --no-install --no-agents-md --no-version-check",
		"mix phx.gen.live Catalog Item items name:string --no-scope",
		"live \"/items\", ItemLive.Index, :index",
		"mix phx.gen.release",
		"MIX_ENV=prod mix compile",
		"MIX_ENV=prod mix assets.deploy",
		"MIX_ENV=prod mix release",
		"FROM ${ALPINE_IMAGE} AS runtime",
		"COPY --from=build --chown=65534:65534 /workspace/app/_build/prod/rel/app ./",
		"USER 65534:65534",
		"CMD [\"sh\", \"-c\", \"/app/bin/migrate && exec /app/bin/server\"]",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing production marker %q", want)
		}
	}
	for _, forbidden := range []string{"mix phx.server", "MIX_ENV=dev", "CMD [\"mix\""} {
		if strings.Contains(dockerfile, forbidden) || strings.Contains(composeFile, forbidden) {
			t.Fatalf("production compatibility probe must not contain development command %q", forbidden)
		}
	}
	for _, want := range []string{
		"127.0.0.1:18104:4000",
		"SECRET_KEY_BASE: ${SECRET_KEY_BASE:?set SECRET_KEY_BASE with sshdock config set}",
		"PHX_HOST: ${PHX_HOST:?set PHX_HOST with sshdock config set}",
		"DATABASE_PATH: /app/data/phoenix_liveview.sqlite3",
		"phoenix_data:/app/data",
		"http://127.0.0.1:4000/items",
		"healthcheck:",
		"restart: unless-stopped",
	} {
		if !strings.Contains(composeFile, want) {
			t.Fatalf("compose.yml missing production marker %q", want)
		}
	}

	// Then its public workflow records exact provenance and covers LiveView reconnect and lifecycle behavior.
	readme := readTextFile(t, filepath.Join(dir, "README.md"))
	for _, want := range []string{
		"Phoenix LiveView framework compatibility probe",
		"phx_new 1.8.9",
		"Phoenix LiveView 1.2.7",
		"hexpm/elixir:1.20.2-erlang-28.5.0.3-alpine-3.23.5@sha256:6f03034e254126f063959873d8d3b811ee92abaabab27b62c53982c4a1034e39",
		"alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40",
		"https://hexdocs.pm/phoenix/Mix.Tasks.Phx.New.html",
		"mix phx.new example_app",
		"config set phoenix SECRET_KEY_BASE",
		"config set phoenix PHX_HOST",
		"git push sshdock main",
		"https://phoenix.example.com/items",
		"secure WebSocket",
		"Attempting to reconnect",
		"sshdock apps health phoenix",
		"sshdock logs phoenix web",
		"sshdock apps restart phoenix",
		"sshdock apps redeploy phoenix",
		"sshdock apps remove phoenix --force",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing workflow or provenance marker %q", want)
		}
	}
}

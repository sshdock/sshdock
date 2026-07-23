package harness

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestPostgreSQLRecipe_contract_when_tunnel_only_and_pinned(t *testing.T) {
	// Given the registered PostgreSQL recipe and its SSH-tunnel-only public contract.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "databases", "postgres")

	// When its Compose model is validated with the values SSHDock supplies at deploy time.
	result, err := compose.ValidateFileWithEnv(filepath.Join(dir, "compose.yml"), map[string]string{
		"POSTGRES_DB":       "sshdock",
		"POSTGRES_USER":     "sshdock",
		"POSTGRES_PASSWORD": "contract-postgres-password",
	})
	if err != nil {
		t.Fatalf("ValidateFileWithEnv: %v", err)
	}
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	readme := readTextFile(t, filepath.Join(dir, "README.md"))

	// Then it remains a one-service, loopback-only official-image recipe with explicit persistence.
	if !slices.Equal(result.Services, []string{"db"}) {
		t.Fatalf("services = %v, want [db]", result.Services)
	}
	for _, want := range []string{
		"postgres:16.14-alpine3.22@sha256:786dab398303b8ce7cb76b407bb21ef2e4dfbbbd4c6abcf3d29b3130467ffdbc",
		"127.0.0.1:18205:5432",
		"POSTGRES_DB: ${POSTGRES_DB:?",
		"POSTGRES_USER: ${POSTGRES_USER:?",
		"POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?",
		"postgres-data:/var/lib/postgresql/data",
		"pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB",
		"restart: unless-stopped",
	} {
		if !strings.Contains(composeFile, want) {
			t.Fatalf("compose.yml missing %q", want)
		}
	}
	for _, forbidden := range []string{"build:", "latest", "0.0.0.0:", "5432:5432"} {
		if strings.Contains(composeFile, forbidden) {
			t.Fatalf("compose.yml contains forbidden value %q", forbidden)
		}
	}
	for _, want := range []string{
		"config set postgres POSTGRES_PASSWORD",
		"ssh -N -L 15432:127.0.0.1:18205",
		"psql \"postgresql://",
		"docker volume rm sshdock_postgres_postgres-data",
		"restricted `sshdock` account disables TCP forwarding",
		"app-scoped private default network",
		"PostgreSQL TLS",
		"firewall or provider allowlisting",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing executable contract %q", want)
		}
	}
}

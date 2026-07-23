package harness

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestSharedPostgreSQLRecipe_contract_when_clients_are_isolated_on_external_network(t *testing.T) {
	// Given the registered shared PostgreSQL example and its operator-owned network contract.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "databases", "shared-postgres")

	// When its Compose model is validated with the values SSHDock supplies at deploy time.
	_, err := compose.ValidateFileWithEnv(filepath.Join(dir, "compose.yml"), map[string]string{
		"POSTGRES_ADMIN_PASSWORD": "contract-admin-password",
		"CLIENT_A_DATABASE_URL":   "postgresql://client_a:contract-client-a-password@shared-postgres:5432/client_a?sslmode=disable",
		"CLIENT_B_DATABASE_URL":   "postgresql://client_b:contract-client-b-password@shared-postgres:5432/client_b?sslmode=disable",
	})
	if err != nil {
		t.Fatalf("ValidateFileWithEnv: %v", err)
	}
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	readme := readTextFile(t, filepath.Join(dir, "README.md"))

	// Then the distinct shared-network boundary stays deliberate and private.
	for _, want := range []string{
		"DATABASE_URL: ${CLIENT_A_DATABASE_URL:?",
		"DATABASE_URL: ${CLIENT_B_DATABASE_URL:?",
		"aliases: [shared-postgres]",
		"external: true",
		"name: sshdock-shared-postgres",
		"REVOKE CONNECT ON DATABASE %I FROM PUBLIC",
	} {
		if !strings.Contains(composeFile, want) {
			t.Fatalf("compose.yml missing %q", want)
		}
	}
	for _, forbidden := range []string{"ports:", "0.0.0.0:", "5432:5432"} {
		if strings.Contains(composeFile, forbidden) {
			t.Fatalf("compose.yml contains forbidden value %q", forbidden)
		}
	}
	for _, want := range []string{
		"docker network create sshdock-shared-postgres",
		"config set shared-postgres CLIENT_A_DATABASE_URL",
		"config set shared-postgres CLIENT_B_DATABASE_URL",
		"cannot connect to Client B's database",
		"cannot resolve `shared-postgres`",
		"sudo sshdock apps remove shared-postgres --force",
		"docker network rm sshdock-shared-postgres",
		"does not manage the shared network",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing executable contract %q", want)
		}
	}
}

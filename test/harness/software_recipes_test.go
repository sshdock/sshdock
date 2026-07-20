package harness

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestWordPressSoftwareRecipe_contract_when_pinned_and_stateful(t *testing.T) {
	// Given the public WordPress recipe and its required SSHDock config.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "software", "wordpress")
	env := map[string]string{
		"WORDPRESS_DB_NAME":          "wordpress",
		"WORDPRESS_DB_USER":          "wordpress",
		"WORDPRESS_DB_PASSWORD":      "contract-password",
		"WORDPRESS_DB_ROOT_PASSWORD": "contract-root-password",
	}

	// When the root Compose file is validated and inspected.
	result, err := compose.ValidateFileWithEnv(filepath.Join(dir, "compose.yml"), env)
	if err != nil {
		t.Fatalf("ValidateFileWithEnv: %v", err)
	}
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	readme := readTextFile(t, filepath.Join(dir, "README.md"))

	// Then it uses the exact official images and the minimal stateful topology.
	if !slices.Equal(result.Services, []string{"db", "web"}) {
		t.Fatalf("services = %v, want [db web]", result.Services)
	}
	for _, want := range []string{
		"wordpress:7.0.1-php8.3-apache@sha256:d40b86dbdfcfad808a2029acf6543c670c4a61c29f70b9d24605e7d0b31ab83d",
		"mariadb:12.3.2-noble@sha256:628f228f0fd5913a220438693576b29b6fe4dc1fa0a1298c0e98579fae28635f",
		"127.0.0.1:18200:80",
		"WORDPRESS_DB_NAME: ${WORDPRESS_DB_NAME:?",
		"WORDPRESS_DB_USER: ${WORDPRESS_DB_USER:?",
		"WORDPRESS_DB_PASSWORD: ${WORDPRESS_DB_PASSWORD:?",
		"MARIADB_ROOT_PASSWORD: ${WORDPRESS_DB_ROOT_PASSWORD:?",
		"condition: service_healthy",
		"wordpress-data:/var/www/html",
		"mariadb-data:/var/lib/mysql",
		"restart: unless-stopped",
	} {
		if !strings.Contains(composeFile, want) {
			t.Fatalf("compose.yml missing %q", want)
		}
	}
	if strings.Count(composeFile, "healthcheck:") != 2 {
		t.Fatalf("healthcheck count = %d, want 2", strings.Count(composeFile, "healthcheck:"))
	}
	if strings.Count(composeFile, "ports:") != 1 {
		t.Fatalf("published-port count = %d, want 1", strings.Count(composeFile, "ports:"))
	}
	for _, forbidden := range []string{"build:", "0.0.0.0:", "3306:3306"} {
		if strings.Contains(composeFile, forbidden) {
			t.Fatalf("compose.yml contains forbidden value %q", forbidden)
		}
	}
	for _, want := range []string{
		"config set wordpress WORDPRESS_DB_ROOT_PASSWORD",
		"WORDPRESS_DB_ROOT_PASSWORD=local-only-root-password",
		"docker volume rm sshdock_wordpress_wordpress-data sshdock_wordpress_mariadb-data",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing executable contract %q", want)
		}
	}
}

func TestGiteaSoftwareRecipe_contract_when_pinned_and_stateful(t *testing.T) {
	// Given the public Gitea recipe and its required SSHDock config.
	root := repoRoot(t)
	dir := filepath.Join(root, "examples", "software", "gitea")
	env := map[string]string{
		"GITEA_DOMAIN":         "gitea.example.com",
		"GITEA_ROOT_URL":       "https://gitea.example.com/",
		"GITEA_SECRET_KEY":     "contract-secret-key",
		"GITEA_INTERNAL_TOKEN": "contract-internal-token",
	}

	// When the root Compose file is validated and inspected.
	result, err := compose.ValidateFileWithEnv(filepath.Join(dir, "compose.yml"), env)
	if err != nil {
		t.Fatalf("ValidateFileWithEnv: %v", err)
	}
	composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
	readme := readTextFile(t, filepath.Join(dir, "README.md"))

	// Then it uses the exact official rootless image and one-service SQLite topology.
	if !slices.Equal(result.Services, []string{"web"}) {
		t.Fatalf("services = %v, want [web]", result.Services)
	}
	for _, want := range []string{
		"docker.gitea.com/gitea:1.27.0-rootless@sha256:414ba5b2b1163480e9ed4213a989cd798579cfa88582a2359303273009b2b852",
		"127.0.0.1:18201:3000",
		"18222:2222",
		"GITEA__server__DOMAIN: ${GITEA_DOMAIN:?",
		"GITEA__server__ROOT_URL: ${GITEA_ROOT_URL:?",
		"GITEA__security__SECRET_KEY: ${GITEA_SECRET_KEY:?",
		"GITEA__security__INTERNAL_TOKEN: ${GITEA_INTERNAL_TOKEN:?",
		"GITEA__database__DB_TYPE: sqlite3",
		"GITEA__server__START_SSH_SERVER: \"true\"",
		"gitea-data:/var/lib/gitea",
		"gitea-config:/etc/gitea",
		"restart: unless-stopped",
	} {
		if !strings.Contains(composeFile, want) {
			t.Fatalf("compose.yml missing %q", want)
		}
	}
	if strings.Count(composeFile, "healthcheck:") != 1 {
		t.Fatalf("healthcheck count = %d, want 1", strings.Count(composeFile, "healthcheck:"))
	}
	if strings.Count(composeFile, "ports:") != 1 {
		t.Fatalf("published-port count = %d, want 1", strings.Count(composeFile, "ports:"))
	}
	for _, forbidden := range []string{"build:", "latest", "postgres:", "mysql:", "mariadb:"} {
		if strings.Contains(composeFile, forbidden) {
			t.Fatalf("compose.yml contains forbidden value %q", forbidden)
		}
	}
	for _, want := range []string{
		"config set gitea GITEA_SECRET_KEY",
		"select SQLite3 and set the path to `/var/lib/gitea/data/gitea.db`",
		"git clone ssh://git@gitea.example.com:18222/acceptance/recipe-proof.git",
		"docker volume rm sshdock_gitea_gitea-data sshdock_gitea_gitea-config",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing executable contract %q", want)
		}
	}
}

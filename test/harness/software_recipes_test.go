package harness

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

type softwareRecipeContract struct {
	name                  string
	directory             string
	env                   map[string]string
	services              []string
	composeRequired       []string
	healthcheckCount      int
	publishedPortSections int
	composeForbidden      []string
	readmeRequired        []string
}

func TestSoftwareRecipes_contract_when_pinned_and_stateful(t *testing.T) {
	// Given the registered software recipes and their explicit public contracts.
	root := repoRoot(t)
	tests := []softwareRecipeContract{
		{
			name:      "WordPress",
			directory: "wordpress",
			env: map[string]string{
				"WORDPRESS_DB_NAME":          "wordpress",
				"WORDPRESS_DB_USER":          "wordpress",
				"WORDPRESS_DB_PASSWORD":      "contract-password",
				"WORDPRESS_DB_ROOT_PASSWORD": "contract-root-password",
			},
			services: []string{"db", "web"},
			composeRequired: []string{
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
			},
			healthcheckCount:      2,
			publishedPortSections: 1,
			composeForbidden:      []string{"build:", "0.0.0.0:", "3306:3306"},
			readmeRequired: []string{
				"config set wordpress WORDPRESS_DB_ROOT_PASSWORD",
				"WORDPRESS_DB_ROOT_PASSWORD=local-only-root-password",
				"docker volume rm sshdock_wordpress_wordpress-data sshdock_wordpress_mariadb-data",
			},
		},
		{
			name:      "Gitea",
			directory: "gitea",
			env: map[string]string{
				"GITEA_DOMAIN":         "gitea.example.com",
				"GITEA_ROOT_URL":       "https://gitea.example.com/",
				"GITEA_SECRET_KEY":     "contract-secret-key",
				"GITEA_INTERNAL_TOKEN": "contract-internal-token",
			},
			services: []string{"web"},
			composeRequired: []string{
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
			},
			healthcheckCount:      1,
			publishedPortSections: 1,
			composeForbidden:      []string{"build:", "latest", "postgres:", "mysql:", "mariadb:"},
			readmeRequired: []string{
				"config set gitea GITEA_SECRET_KEY",
				"select SQLite3 and set the path to `/var/lib/gitea/data/gitea.db`",
				"git clone ssh://git@gitea.example.com:18222/acceptance/recipe-proof.git",
				"docker volume rm sshdock_gitea_gitea-data sshdock_gitea_gitea-config",
				"`GITEA__security__*`",
				"`/etc/gitea/app.ini`",
				"plaintext copies",
				"Docker container inspection",
				"`gitea-config` volume",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := filepath.Join(root, "examples", "software", test.directory)

			// When the root Compose file is validated and inspected.
			result, err := compose.ValidateFileWithEnv(filepath.Join(dir, "compose.yml"), test.env)
			if err != nil {
				t.Fatalf("ValidateFileWithEnv: %v", err)
			}
			composeFile := readTextFile(t, filepath.Join(dir, "compose.yml"))
			readme := readTextFile(t, filepath.Join(dir, "README.md"))

			// Then the recipe preserves its exact topology, pins, and executable guidance.
			if !slices.Equal(result.Services, test.services) {
				t.Fatalf("services = %v, want %v", result.Services, test.services)
			}
			for _, want := range test.composeRequired {
				if !strings.Contains(composeFile, want) {
					t.Fatalf("compose.yml missing %q", want)
				}
			}
			if count := strings.Count(composeFile, "healthcheck:"); count != test.healthcheckCount {
				t.Fatalf("healthcheck count = %d, want %d", count, test.healthcheckCount)
			}
			if count := strings.Count(composeFile, "ports:"); count != test.publishedPortSections {
				t.Fatalf("published-port count = %d, want %d", count, test.publishedPortSections)
			}
			for _, forbidden := range test.composeForbidden {
				if strings.Contains(composeFile, forbidden) {
					t.Fatalf("compose.yml contains forbidden value %q", forbidden)
				}
			}
			for _, want := range test.readmeRequired {
				if !strings.Contains(readme, want) {
					t.Fatalf("README.md missing executable contract %q", want)
				}
			}
		})
	}
}

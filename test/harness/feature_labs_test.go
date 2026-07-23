package harness

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestConfigAndRedeployFeatureLab_contract_when_overlaying_nestjs_probe(t *testing.T) {
	// Given the registered NestJS compatibility probe and its config lab.
	root := repoRoot(t)
	labDir := filepath.Join(root, "examples", "labs", "config-and-redeploy")

	// When the lab's executable overlay and public registration are inspected.
	entries, err := os.ReadDir(labDir)
	if err != nil {
		t.Fatalf("ReadDir feature lab: %v", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("feature lab contains nested directory %q", entry.Name())
		}
		files = append(files, entry.Name())
	}
	slices.Sort(files)
	if want := []string{"README.md", "config.patch"}; !slices.Equal(files, want) {
		t.Fatalf("feature lab files = %#v, want %#v", files, want)
	}

	patch := readTextFile(t, filepath.Join(labDir, "config.patch"))
	for _, want := range []string{
		"diff --git a/compose.yml b/compose.yml",
		"CONFIG_LAB_SECRET: ${CONFIG_LAB_SECRET:?set CONFIG_LAB_SECRET with sshdock config set}",
	} {
		if !strings.Contains(patch, want) {
			t.Fatalf("config patch missing %q", want)
		}
	}

	readme := readTextFile(t, filepath.Join(labDir, "README.md"))
	for _, want := range []string{
		"examples/frameworks/nestjs",
		"git apply config.patch",
		"config set config-and-redeploy CONFIG_LAB_SECRET",
		"config import config-and-redeploy",
		"config list config-and-redeploy",
		"config keys config-and-redeploy",
		"config get config-and-redeploy CONFIG_LAB_SECRET",
		"config unset config-and-redeploy CONFIG_LAB_TEMP",
		"sshdock apps redeploy config-and-redeploy",
		"sshdock deployments list config-and-redeploy",
		"sshdock events list config-and-redeploy",
		"sshdock apps health config-and-redeploy",
		"sshdock logs config-and-redeploy web",
		"sshdock apps remove config-and-redeploy --force",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing workflow marker %q", want)
		}
	}

	guide := readTextFile(t, filepath.Join(root, "docs", "EXAMPLES.md"))
	if !strings.Contains(guide, "examples/labs/config-and-redeploy") {
		t.Fatal("public examples guide does not register the config-and-redeploy feature lab")
	}
}

func TestConfigAndRedeployFeatureLab_patch_rejects_missing_config(t *testing.T) {
	// Given the executable config-lab overlay.
	composePath, _ := writeConfigAndRedeployLabCompose(t)

	// When Compose validates the overlay without stored config.
	_, err := compose.ValidateFile(composePath)

	// Then it stops before application start with an actionable missing-key error.
	if err == nil || !strings.Contains(err.Error(), "CONFIG_LAB_SECRET") {
		t.Fatalf("missing-config validation error = %v", err)
	}
}

func TestConfigAndRedeployFeatureLab_patch_accepts_config_from_environment(t *testing.T) {
	// Given the executable config-lab overlay.
	composePath, overlaid := writeConfigAndRedeployLabCompose(t)

	// When the overlay receives the stored value through the process environment.
	validation, err := compose.ValidateFileWithEnv(composePath, map[string]string{"CONFIG_LAB_SECRET": "redaction-test-value"})

	// Then the canonical service model remains valid without storing that value in the file.
	if err != nil {
		t.Fatalf("ValidateFileWithEnv overlaid Compose: %v", err)
	}
	if !slices.Equal(validation.Services, []string{"web"}) {
		t.Fatalf("overlaid services = %#v, want [web]", validation.Services)
	}
	if strings.Contains(overlaid, "redaction-test-value") {
		t.Fatal("overlaid Compose contains the stored config value")
	}
}

func writeConfigAndRedeployLabCompose(t *testing.T) (string, string) {
	t.Helper()
	root := repoRoot(t)
	canonical := readTextFile(t, filepath.Join(root, "examples", "frameworks", "nestjs", "compose.yml"))
	patch := readTextFile(t, filepath.Join(root, "examples", "labs", "config-and-redeploy", "config.patch"))
	const original = "    build:\n      context: .\n    ports:\n"
	const patched = "    build:\n      context: .\n    environment:\n      CONFIG_LAB_SECRET: ${CONFIG_LAB_SECRET:?set CONFIG_LAB_SECRET with sshdock config set}\n    ports:\n"
	const wantPatch = "diff --git a/compose.yml b/compose.yml\n--- a/compose.yml\n+++ b/compose.yml\n@@ -3,5 +3,7 @@ services:\n     build:\n       context: .\n+    environment:\n+      CONFIG_LAB_SECRET: ${CONFIG_LAB_SECRET:?set CONFIG_LAB_SECRET with sshdock config set}\n     ports:\n       - \"127.0.0.1:18101:3000\"\n     restart: unless-stopped\n"
	if patch != wantPatch {
		t.Fatalf("config patch differs from its executable contract:\n%s", patch)
	}
	if strings.Count(canonical, original) != 1 {
		t.Fatalf("NestJS Compose overlay target count = %d, want 1", strings.Count(canonical, original))
	}
	overlaid := strings.Replace(canonical, original, patched, 1)
	composePath := filepath.Join(t.TempDir(), "compose.yml")
	if err := os.WriteFile(composePath, []byte(overlaid), 0o600); err != nil {
		t.Fatalf("WriteFile overlaid Compose: %v", err)
	}
	return composePath, overlaid
}

func TestFailedDeployAndGitRecoveryFeatureLab_contract_when_overlaying_nextjs_probe(t *testing.T) {
	// Given the registered Next.js compatibility probe and its recovery lab.
	root := repoRoot(t)
	labDir := filepath.Join(root, "examples", "labs", "failed-deploy-and-git-recovery")

	// When the lab's executable overlay and public registration are inspected.
	entries, err := os.ReadDir(labDir)
	if err != nil {
		t.Fatalf("ReadDir feature lab: %v", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("feature lab contains nested directory %q", entry.Name())
		}
		files = append(files, entry.Name())
	}
	slices.Sort(files)
	if want := []string{"README.md", "failure.patch"}; !slices.Equal(files, want) {
		t.Fatalf("feature lab files = %#v, want %#v", files, want)
	}

	patch := readTextFile(t, filepath.Join(labDir, "failure.patch"))
	for _, want := range []string{
		"diff --git a/compose.yml b/compose.yml",
		"dockerfile: Dockerfile.failure",
	} {
		if !strings.Contains(patch, want) {
			t.Fatalf("failure patch missing %q", want)
		}
	}

	readme := readTextFile(t, filepath.Join(labDir, "README.md"))
	for _, want := range []string{
		"examples/frameworks/nextjs",
		"git apply failure.patch",
		"git push sshdock main",
		"ssh sshdock@sshdock.example.com apps health failed-deploy-and-git-recovery",
		"sshdock apps health failed-deploy-and-git-recovery",
		"sshdock releases list failed-deploy-and-git-recovery",
		"sshdock deployments list failed-deploy-and-git-recovery",
		"sshdock events list failed-deploy-and-git-recovery",
		"git push --force sshdock \"$GOOD_COMMIT:main\"",
		"sshdock apps remove failed-deploy-and-git-recovery --force",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing workflow marker %q", want)
		}
	}

	guide := readTextFile(t, filepath.Join(root, "docs", "EXAMPLES.md"))
	if !strings.Contains(guide, "examples/labs/failed-deploy-and-git-recovery") {
		t.Fatal("public examples guide does not register the failed-deploy-and-git-recovery feature lab")
	}
}

func TestFailedDeployAndGitRecoveryFeatureLab_patch_builds_with_a_missing_dockerfile(t *testing.T) {
	// Given the executable recovery-lab overlay.
	composePath, overlaid := writeFailedDeployAndGitRecoveryLabCompose(t)

	// When SSHDock validates the patched Compose model before invoking Docker.
	validation, err := compose.ValidateFile(composePath)

	// Then Compose is valid but its build points at the controlled absent Dockerfile.
	if err != nil {
		t.Fatalf("ValidateFile overlaid Compose: %v", err)
	}
	if !slices.Equal(validation.Services, []string{"web"}) {
		t.Fatalf("overlaid services = %#v, want [web]", validation.Services)
	}
	if !strings.Contains(overlaid, "dockerfile: Dockerfile.failure") {
		t.Fatal("overlaid Compose does not select the controlled failure Dockerfile")
	}

	root := repoRoot(t)
	missingDockerfile := filepath.Join(root, "examples", "frameworks", "nextjs", "Dockerfile.failure")
	if _, statErr := os.Stat(missingDockerfile); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("controlled failure Dockerfile stat error = %v, want not exist", statErr)
	}
}

func writeFailedDeployAndGitRecoveryLabCompose(t *testing.T) (string, string) {
	t.Helper()
	root := repoRoot(t)
	canonical := readTextFile(t, filepath.Join(root, "examples", "frameworks", "nextjs", "compose.yml"))
	patch := readTextFile(t, filepath.Join(root, "examples", "labs", "failed-deploy-and-git-recovery", "failure.patch"))
	const original = "    build:\n      context: .\n    ports:\n"
	const patched = "    build:\n      context: .\n      dockerfile: Dockerfile.failure\n    ports:\n"
	const wantPatch = "diff --git a/compose.yml b/compose.yml\n--- a/compose.yml\n+++ b/compose.yml\n@@ -2,5 +2,6 @@ services:\n   web:\n     build:\n       context: .\n+      dockerfile: Dockerfile.failure\n     ports:\n       - \"127.0.0.1:18100:3000\"\n     healthcheck:\n"
	if patch != wantPatch {
		t.Fatalf("failure patch differs from its executable contract:\n%s", patch)
	}
	if strings.Count(canonical, original) != 1 {
		t.Fatalf("Next.js Compose overlay target count = %d, want 1", strings.Count(canonical, original))
	}
	overlaid := strings.Replace(canonical, original, patched, 1)
	composePath := filepath.Join(t.TempDir(), "compose.yml")
	if err := os.WriteFile(composePath, []byte(overlaid), 0o600); err != nil {
		t.Fatalf("WriteFile overlaid Compose: %v", err)
	}
	return composePath, overlaid
}

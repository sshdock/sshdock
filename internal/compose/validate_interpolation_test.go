package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFileAllowsInterpolatedExtendsDefaultingToSelectedRoot(t *testing.T) {
	// Given
	t.Setenv("SSHDOCK_TEST_ROOT_COMPOSE", "")
	path := writeValidationCompose(t, `
services:
  base:
    image: example/base:latest
  web:
    extends:
      file: ${SSHDOCK_TEST_ROOT_COMPOSE:-compose.yml}
      service: base
`)

	// When
	_, err := ValidateFile(path)

	// Then
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
}

func TestValidateFileWithEnvRejectsInterpolatedExternalExtends(t *testing.T) {
	// Given
	path := writeValidationCompose(t, `
services:
  web:
    extends:
      file: ${SSHDOCK_TEST_ROOT_COMPOSE:-compose.yml}
      service: base
`)

	// When
	_, err := ValidateFileWithEnv(path, map[string]string{"SSHDOCK_TEST_ROOT_COMPOSE": "shared.compose.yml"})

	// Then
	if err == nil || !strings.Contains(err.Error(), "SSHDOCK_TEST_ROOT_COMPOSE") {
		t.Fatalf("ValidateFileWithEnv error = %q, want raw interpolation reference", err)
	}
	if strings.Contains(err.Error(), "shared.compose.yml") {
		t.Fatalf("ValidateFileWithEnv error disclosed app config: %q", err)
	}
}

func TestValidateFileWithEnvAllowsStandardSameRootInterpolationForms(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		env       map[string]string
	}{
		{name: "direct variable", reference: "${SSHDOCK_TEST_ROOT_COMPOSE}", env: map[string]string{"SSHDOCK_TEST_ROOT_COMPOSE": "compose.yml"}},
		{name: "embedded default", reference: "${SSHDOCK_TEST_ROOT_DIR:-.}/compose.yml", env: map[string]string{}},
		{name: "default when unset", reference: "${SSHDOCK_TEST_ROOT_COMPOSE-compose.yml}", env: map[string]string{}},
		{name: "project working directory", reference: "${PWD}/compose.yml", env: map[string]string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeValidationCompose(t, "services:\n  base:\n    image: example/base:latest\n  web:\n    extends:\n      file: "+test.reference+"\n      service: base\n")
			if _, err := ValidateFileWithEnv(path, test.env); err != nil {
				t.Fatalf("ValidateFileWithEnv: %v", err)
			}
		})
	}
}

func TestValidateFileRejectsExternalExtendsResolvedFromProjectDotEnv(t *testing.T) {
	// Given
	path := writeValidationCompose(t, `
services:
  web:
    extends:
      file: ${SSHDOCK_TEST_ROOT_COMPOSE:-compose.yml}
      service: base
`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), ".env"), []byte("SSHDOCK_TEST_ROOT_COMPOSE=shared.compose.yml\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.env): %v", err)
	}

	// When
	_, err := ValidateFile(path)

	// Then
	if err == nil || !strings.Contains(err.Error(), "SSHDOCK_TEST_ROOT_COMPOSE") {
		t.Fatalf("ValidateFile error = %q, want raw interpolation reference", err)
	}
	if strings.Contains(err.Error(), "shared.compose.yml") {
		t.Fatalf("ValidateFile error disclosed .env value: %q", err)
	}
}

func TestValidateFileWithEnvDoesNotDiscloseInterpolatedConfigValue(t *testing.T) {
	// Given
	path := writeValidationCompose(t, `
include: ${API_TOKEN}
services: {}
`)

	// When
	_, err := ValidateFileWithEnv(path, map[string]string{"API_TOKEN": "super-secret-token"})

	// Then
	if err == nil || !strings.Contains(err.Error(), "${API_TOKEN}") {
		t.Fatalf("ValidateFileWithEnv error = %q, want raw interpolation reference", err)
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("ValidateFileWithEnv error disclosed app config: %q", err)
	}
}

func TestValidateFileWithEnvDoesNotDiscloseSecretInInterpolationError(t *testing.T) {
	// Given
	path := writeValidationCompose(t, `
services:
  web:
    image: ${MISSING_IMAGE:?token ${API_TOKEN}}
`)

	// When
	_, err := ValidateFileWithEnv(path, map[string]string{"API_TOKEN": "super-secret-token"})

	// Then
	if err == nil || !strings.Contains(err.Error(), "MISSING_IMAGE") {
		t.Fatalf("ValidateFileWithEnv error = %q, want missing variable name", err)
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("ValidateFileWithEnv interpolation error disclosed app config: %q", err)
	}
}

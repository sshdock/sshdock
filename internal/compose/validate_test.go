package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFileAllowsDockerComposeToOwnStandardApplicationFields(t *testing.T) {
	// Given
	path := writeValidationCompose(t, `
x-service: &service
  image: example/web:latest
  command: ["./serve", "--port", "8080"]
  labels:
    com.example.role: web
  networks: [frontend]
  configs: [app-config]
  secrets: [app-secret]
  deploy:
    resources:
      limits:
        cpus: "0.50"
        memory: 256M
services:
  web:
    <<: *service
networks:
  frontend: {}
configs:
  app-config:
    file: ./app.conf
secrets:
  app-secret:
    file: ./app.secret
`)

	// When
	result, err := ValidateFile(path)

	// Then
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if strings.Join(result.Services, ",") != "web" {
		t.Fatalf("Services = %#v, want [web]", result.Services)
	}
}

func TestValidateFileRejectsTopLevelIncludeWithActionableGuidance(t *testing.T) {
	// Given
	path := writeValidationCompose(t, `
include:
  - shared.compose.yml
services:
  web:
    image: example/web:latest
`)

	// When
	_, err := ValidateFile(path)

	// Then
	if err == nil {
		t.Fatal("ValidateFile error = nil, want top-level include rejection")
	}
	for _, want := range []string{"top-level include", "shared.compose.yml", "external Compose files are not supported"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateFile error = %q, want %q", err, want)
		}
	}
}

func TestValidateFileRejectsServiceExtendsExternalFileWithActionableGuidance(t *testing.T) {
	// Given
	path := writeValidationCompose(t, `
services:
  web:
    extends:
      file: shared.compose.yml
      service: base
`)

	// When
	_, err := ValidateFile(path)

	// Then
	if err == nil {
		t.Fatal("ValidateFile error = nil, want external extends rejection")
	}
	for _, want := range []string{"services.web.extends.file", "shared.compose.yml", "external Compose files are not supported"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateFile error = %q, want %q", err, want)
		}
	}
}

func TestValidateFileRejectsExternalFilesHiddenBySameFileAnchors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "top-level include merged from anchor",
			content: `
x-root: &root
  include: shared.compose.yml
<<: *root
services:
  web:
    image: example/web:latest
`,
			want: "top-level include",
		},
		{
			name: "extends mapping supplied by alias",
			content: `
x-extends: &external
  file: shared.compose.yml
  service: base
services:
  web:
    extends: *external
`,
			want: "services.web.extends.file",
		},
		{
			name: "extends merged into service",
			content: `
x-service: &external
  extends:
    file: shared.compose.yml
    service: base
services:
  web:
    <<: *external
`,
			want: "services.web.extends.file",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			path := writeValidationCompose(t, test.content)

			// When
			_, err := ValidateFile(path)

			// Then
			if err == nil {
				t.Fatal("ValidateFile error = nil, want anchored external file rejection")
			}
			for _, want := range []string{test.want, "shared.compose.yml", "external Compose files are not supported"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("ValidateFile error = %q, want %q", err, want)
				}
			}
		})
	}
}

func TestValidateFileLeavesSameFileExtendsToDockerCompose(t *testing.T) {
	// Given
	path := writeValidationCompose(t, `
services:
  base:
    image: example/base:latest
  web:
    extends:
      service: base
`)

	// When
	result, err := ValidateFile(path)

	// Then
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if strings.Join(result.Services, ",") != "base,web" {
		t.Fatalf("Services = %#v, want [base web]", result.Services)
	}
}

func TestValidateFileLeavesExplicitSelectedRootExtendsToDockerCompose(t *testing.T) {
	// Given
	path := writeValidationCompose(t, `
services:
  base:
    image: example/base:latest
  web:
    extends:
      file: ./compose.yml
      service: base
`)

	// When
	result, err := ValidateFile(path)

	// Then
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if strings.Join(result.Services, ",") != "base,web" {
		t.Fatalf("Services = %#v, want [base web]", result.Services)
	}
}

func TestValidateFileRejectsInvalidYAMLNeededForPolicyInspection(t *testing.T) {
	// Given
	path := writeValidationCompose(t, "services:\n  web: [\n")

	// When
	_, err := ValidateFile(path)

	// Then
	if err == nil || !strings.Contains(err.Error(), "invalid YAML") {
		t.Fatalf("ValidateFile error = %q, want invalid YAML guidance", err)
	}
}

func writeValidationCompose(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "compose.yml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

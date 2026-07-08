package compose

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFileAcceptsSupportedSubsetAndExtractsServices(t *testing.T) {
	result, err := ValidateFile(fixturePath("valid", "compose.yml"))
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}

	want := []string{"api", "web"}
	if strings.Join(result.Services, ",") != strings.Join(want, ",") {
		t.Fatalf("Services = %#v, want %#v", result.Services, want)
	}
}

func TestValidateFileRequiresServices(t *testing.T) {
	_, err := ValidateFile(fixturePath("invalid", "empty-services.yml"))
	if err == nil {
		t.Fatal("ValidateFile error = nil, want error")
	}
	if !strings.Contains(err.Error(), "at least one service") {
		t.Fatalf("ValidateFile error = %q, want service count guidance", err)
	}
}

func TestValidateFileRejectsUnsupportedTopLevelFields(t *testing.T) {
	_, err := ValidateFile(fixturePath("invalid", "unsupported-top-level.yml"))
	if err == nil {
		t.Fatal("ValidateFile error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unsupported top-level field") || !strings.Contains(err.Error(), "networks") {
		t.Fatalf("ValidateFile error = %q, want unsupported top-level field name", err)
	}
	if !strings.Contains(err.Error(), "docs/COMPOSE_SUPPORT.md") {
		t.Fatalf("ValidateFile error = %q, want Compose support doc reference", err)
	}
}

func TestValidateFileRejectsUnsupportedServiceFields(t *testing.T) {
	_, err := ValidateFile(fixturePath("invalid", "unsupported-service-field.yml"))
	if err == nil {
		t.Fatal("ValidateFile error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unsupported field") || !strings.Contains(err.Error(), "web.command") {
		t.Fatalf("ValidateFile error = %q, want unsupported service field path", err)
	}
	if !strings.Contains(err.Error(), "docs/COMPOSE_SUPPORT.md") {
		t.Fatalf("ValidateFile error = %q, want Compose support doc reference", err)
	}
}

func TestValidateFileRejectsInvalidYAML(t *testing.T) {
	_, err := ValidateFile(fixturePath("invalid", "invalid-yaml.yml"))
	if err == nil {
		t.Fatal("ValidateFile error = nil, want error")
	}
	if !strings.Contains(err.Error(), "invalid YAML") {
		t.Fatalf("ValidateFile error = %q, want invalid YAML guidance", err)
	}
}

func fixturePath(parts ...string) string {
	all := append([]string{"..", "..", "test", "fixtures", "compose"}, parts...)
	return filepath.Join(all...)
}

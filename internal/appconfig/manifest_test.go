package appconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadManifestRequiredKeys(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, ".sshdock.yml")
	if err := os.WriteFile(manifestPath, []byte(`config:
  required:
    - DATABASE_URL
    - name: STRIPE_SECRET_KEY
      scope: web
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifest, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	want := []RequiredKey{
		{Name: "DATABASE_URL"},
		{Name: "STRIPE_SECRET_KEY", Scope: "web"},
	}
	if !reflect.DeepEqual(manifest.Required, want) {
		t.Fatalf("required keys = %#v, want %#v", manifest.Required, want)
	}
}

func TestLoadManifestMissingFileHasNoRequirements(t *testing.T) {
	manifest, err := LoadManifest(t.TempDir())
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(manifest.Required) != 0 {
		t.Fatalf("required keys = %#v, want none", manifest.Required)
	}
}

func TestLoadManifestRejectsInvalidKeyName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".sshdock.yml"), []byte(`config:
  required:
    - bad-key
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("LoadManifest error = nil, want invalid key error")
	}
}

package compose

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestDetectFilePrefersComposeYAML(t *testing.T) {
	projectDir := filepath.Join("..", "..", "test", "fixtures", "compose", "both")

	got, err := DetectFile(projectDir)
	if err != nil {
		t.Fatalf("DetectFile: %v", err)
	}

	want := filepath.Join(projectDir, "compose.yml")
	if got != want {
		t.Fatalf("DetectFile = %q, want %q", got, want)
	}
}

func TestDetectFileFallsBackToDockerComposeYAML(t *testing.T) {
	projectDir := filepath.Join("..", "..", "test", "fixtures", "compose", "docker-compose-only")

	got, err := DetectFile(projectDir)
	if err != nil {
		t.Fatalf("DetectFile: %v", err)
	}

	want := filepath.Join(projectDir, "docker-compose.yml")
	if got != want {
		t.Fatalf("DetectFile = %q, want %q", got, want)
	}
}

func TestDetectFileReturnsClearErrorWhenMissing(t *testing.T) {
	projectDir := filepath.Join("..", "..", "test", "fixtures", "compose", "missing")

	_, err := DetectFile(projectDir)
	if !errors.Is(err, ErrComposeFileNotFound) {
		t.Fatalf("DetectFile error = %v, want ErrComposeFileNotFound", err)
	}
	if err == nil || !containsAll(err.Error(), []string{"compose.yml", "docker-compose.yml", projectDir}) {
		t.Fatalf("DetectFile error = %q, want filenames and project dir", err)
	}
}

func containsAll(value string, needles []string) bool {
	for _, needle := range needles {
		if !contains(value, needle) {
			return false
		}
	}
	return true
}

func contains(value string, needle string) bool {
	for start := 0; start+len(needle) <= len(value); start++ {
		if value[start:start+len(needle)] == needle {
			return true
		}
	}
	return false
}

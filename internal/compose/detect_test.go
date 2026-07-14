package compose

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var composeCandidateNames = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

func TestDetectFileAcceptsEachConventionalRootComposeName(t *testing.T) {
	for _, name := range composeCandidateNames {
		t.Run(name, func(t *testing.T) {
			// Given
			projectDir := t.TempDir()
			path := filepath.Join(projectDir, name)
			if err := os.WriteFile(path, []byte("services: {}\n"), 0o644); err != nil {
				t.Fatalf("WriteFile(%s): %v", name, err)
			}

			// When
			got, err := DetectFile(projectDir)

			// Then
			if err != nil {
				t.Fatalf("DetectFile: %v", err)
			}
			if got != path {
				t.Fatalf("DetectFile = %q, want %q", got, path)
			}
		})
	}
}

func TestDetectFileReturnsExpectedNamesWhenMissing(t *testing.T) {
	// Given
	projectDir := t.TempDir()

	// When
	_, err := DetectFile(projectDir)

	// Then
	if !errors.Is(err, ErrComposeFileNotFound) {
		t.Fatalf("DetectFile error = %v, want ErrComposeFileNotFound", err)
	}
	for _, name := range composeCandidateNames {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("DetectFile error = %q, want expected filename %q", err, name)
		}
	}
}

func TestDetectFileIgnoresDirectoriesMasqueradingAsComposeFiles(t *testing.T) {
	// Given
	projectDir := t.TempDir()
	for _, name := range composeCandidateNames {
		if err := os.Mkdir(filepath.Join(projectDir, name), 0o755); err != nil {
			t.Fatalf("Mkdir(%s): %v", name, err)
		}
	}

	// When
	_, err := DetectFile(projectDir)

	// Then
	if !errors.Is(err, ErrComposeFileNotFound) {
		t.Fatalf("DetectFile error = %v, want ErrComposeFileNotFound", err)
	}
}

func TestDetectFileRejectsSymlinkComposeCandidates(t *testing.T) {
	tests := []struct {
		name   string
		target func(t *testing.T) string
	}{
		{
			name: "regular file target",
			target: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "outside.yml")
				if err := os.WriteFile(path, []byte("services: {}\n"), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
				return path
			},
		},
		{name: "directory target", target: func(t *testing.T) string { return t.TempDir() }},
		{name: "dangling target", target: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.yml") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			projectDir := t.TempDir()
			if err := os.Symlink(test.target(t), filepath.Join(projectDir, "compose.yml")); err != nil {
				t.Fatalf("Symlink: %v", err)
			}

			// When
			_, err := DetectFile(projectDir)

			// Then
			if !errors.Is(err, ErrComposeFileNotFound) {
				t.Fatalf("DetectFile error = %v, want ErrComposeFileNotFound", err)
			}
		})
	}
}

func TestDetectFileRejectsEveryMultipleCandidateCombination(t *testing.T) {
	tests := []struct {
		name  string
		files []string
	}{
		{name: "compose pair", files: []string{"compose.yaml", "compose.yml"}},
		{name: "compose yaml and docker yaml", files: []string{"compose.yaml", "docker-compose.yaml"}},
		{name: "compose yaml and docker yml", files: []string{"compose.yaml", "docker-compose.yml"}},
		{name: "compose yml and docker yaml", files: []string{"compose.yml", "docker-compose.yaml"}},
		{name: "compose yml and docker yml", files: []string{"compose.yml", "docker-compose.yml"}},
		{name: "docker pair", files: []string{"docker-compose.yaml", "docker-compose.yml"}},
		{name: "all except docker yml", files: []string{"compose.yaml", "compose.yml", "docker-compose.yaml"}},
		{name: "all except docker yaml", files: []string{"compose.yaml", "compose.yml", "docker-compose.yml"}},
		{name: "all except compose yml", files: []string{"compose.yaml", "docker-compose.yaml", "docker-compose.yml"}},
		{name: "all except compose yaml", files: []string{"compose.yml", "docker-compose.yaml", "docker-compose.yml"}},
		{name: "all four", files: composeCandidateNames},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			projectDir := t.TempDir()
			for _, name := range test.files {
				if err := os.WriteFile(filepath.Join(projectDir, name), []byte("services: {}\n"), 0o644); err != nil {
					t.Fatalf("WriteFile(%s): %v", name, err)
				}
			}

			// When
			_, err := DetectFile(projectDir)

			// Then
			if !errors.Is(err, ErrMultipleComposeFiles) {
				t.Fatalf("DetectFile error = %v, want ErrMultipleComposeFiles", err)
			}
			for _, name := range test.files {
				if !strings.Contains(err.Error(), name) {
					t.Fatalf("DetectFile error = %q, want conflicting filename %q", err, name)
				}
			}
		})
	}
}

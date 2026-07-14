package compose

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrComposeFileNotFound  = errors.New("compose file not found")
	ErrMultipleComposeFiles = errors.New("multiple compose files found")
)

var composeFileCandidates = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

func DetectFile(projectDir string) (string, error) {
	found := make([]string, 0, len(composeFileCandidates))
	for _, name := range composeFileCandidates {
		path := filepath.Join(projectDir, name)
		info, err := os.Lstat(path)
		if err == nil && info.Mode().IsRegular() {
			found = append(found, name)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect Compose candidate %s: %w", path, err)
		}
	}

	switch len(found) {
	case 0:
		return "", fmt.Errorf("%w in %s: expected exactly one of %s", ErrComposeFileNotFound, projectDir, strings.Join(composeFileCandidates, ", "))
	case 1:
		return filepath.Join(projectDir, found[0]), nil
	default:
		return "", fmt.Errorf("%w in %s: conflicting files: %s; keep exactly one", ErrMultipleComposeFiles, projectDir, strings.Join(found, ", "))
	}
}

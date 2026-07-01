package compose

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrComposeFileNotFound = errors.New("compose file not found")

func DetectFile(projectDir string) (string, error) {
	for _, name := range []string{"compose.yml", "docker-compose.yml"} {
		path := filepath.Join(projectDir, name)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}

	return "", fmt.Errorf("%w in %s: expected compose.yml or docker-compose.yml", ErrComposeFileNotFound, projectDir)
}

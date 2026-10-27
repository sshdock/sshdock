package gitrecv

import (
	"path/filepath"
)

func BareRepoPath(appsDir string, appName string) string {
	return filepath.Join(appsDir, appName, "repo.git")
}

package gitrecv

import (
	"path/filepath"
)

type PushEvent struct {
	AppName   string
	RepoPath  string
	Branch    string
	CommitSHA string
}

func BareRepoPath(appsDir string, appName string) string {
	return filepath.Join(appsDir, appName, "repo.git")
}

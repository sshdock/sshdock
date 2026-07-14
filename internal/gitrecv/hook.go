package gitrecv

import (
	"fmt"
	"strings"
)

func ParsePostReceiveLine(appName string, repoPath string, line string) (PushEvent, error) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return PushEvent{}, fmt.Errorf("post-receive line must contain old sha, new sha, and ref")
	}

	branch, err := branchFromRef(fields[2])
	if err != nil {
		return PushEvent{}, err
	}

	return PushEvent{
		AppName:   appName,
		RepoPath:  repoPath,
		Branch:    branch,
		CommitSHA: fields[1],
	}, nil
}

func branchFromRef(ref string) (string, error) {
	if ref != mainRef {
		return "", fmt.Errorf("unsupported destination %q: only remote %s is deployable", ref, mainRef)
	}
	return "main", nil
}

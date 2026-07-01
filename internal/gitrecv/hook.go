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
	const prefix = "refs/heads/"
	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("unsupported git ref %q: expected refs/heads/<branch>", ref)
	}

	branch := strings.TrimPrefix(ref, prefix)
	if branch == "" {
		return "", fmt.Errorf("git branch name is empty")
	}

	return branch, nil
}

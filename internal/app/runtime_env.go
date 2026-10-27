package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sshdock/sshdock/internal/compose"
)

// ResolveRuntimeEnv binds operational Compose commands to the checked-out
// revision. Remote main may already point at a newer, queued deployment.
func ResolveRuntimeEnv(ctx context.Context, model App, resolver configResolver) (map[string]string, error) {
	var env map[string]string
	if resolver != nil {
		var err error
		env, err = resolver.ResolveEnv(ctx, model.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve app config for app %q: %w", model.ID, err)
		}
	}
	sha, err := checkedOutCommit(model.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve checked-out revision for app %q: %w; redeploy current remote main", model.ID, err)
	}
	return compose.WithGitSHA(env, sha), nil
}

// Managed worktrees leave the bare repository's HEAD detached at the checkout.
// A symbolic HEAD or a missing HEAD has no checked-out revision yet.
func checkedOutCommit(repoPath string) (string, error) {
	if repoPath == "" {
		return "", nil
	}
	head, err := os.ReadFile(filepath.Join(repoPath, "HEAD"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(head))
	if strings.HasPrefix(sha, "ref: ") {
		return "", nil
	}
	decoded, err := hex.DecodeString(sha)
	if err != nil || (len(decoded) != 20 && len(decoded) != 32) {
		return "", fmt.Errorf("repository HEAD is not a full commit ID")
	}
	return strings.ToLower(sha), nil
}

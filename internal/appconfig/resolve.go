package appconfig

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sshdock/sshdock/internal/compose"
)

type decryptedEntry struct {
	ref   ConfigRef
	value string
}

func (s *Service) ResolveEnv(ctx context.Context, appID string) (map[string]string, error) {
	values, err := s.decryptedValues(ctx, appID)
	if err != nil {
		return nil, err
	}
	env := make(map[string]string)
	for _, value := range values {
		if isReservedConfigName(value.ref.Name) {
			return nil, reservedConfigNameError(value.ref.Name)
		}
		env[value.ref.Name] = value.value
	}
	return env, nil
}

func (s *Service) ResolveAppConfig(ctx context.Context, appID string) (map[string]string, error) {
	env, err := s.ResolveEnv(ctx, appID)
	if err != nil {
		return nil, err
	}
	model, err := s.store.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	sha, err := checkedOutCommit(model.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve checked-out revision for app %q: %w; redeploy current remote main", appID, err)
	}
	return compose.WithGitSHA(env, sha), nil
}

// SSHDock checks out full commit IDs into its worktree, leaving the bare
// repository's HEAD detached. A newly accepted main may still be queued, so
// resolving refs/heads/main here would select the wrong revision for operations.
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
		return "", nil // No detached checkout yet; do not use a pending branch tip.
	}
	decoded, err := hex.DecodeString(sha)
	if err != nil || (len(decoded) != 20 && len(decoded) != 32) {
		return "", fmt.Errorf("repository HEAD is not a full commit ID")
	}
	return strings.ToLower(sha), nil
}

func (s *Service) RedactionValues(ctx context.Context, appID string) (map[string]string, error) {
	entries, err := s.decryptedValues(ctx, appID)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		values[entry.ref.display()] = entry.value
	}
	return values, nil
}

func (s *Service) decryptedValues(ctx context.Context, appID string) ([]decryptedEntry, error) {
	if err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	storedValues, err := s.store.ListAppConfigValues(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("list config values for app %q: %w", appID, err)
	}
	if len(storedValues) == 0 {
		return nil, nil
	}
	key, err := LoadHostKey(s.keyPath)
	if err != nil {
		return nil, err
	}
	values := make([]decryptedEntry, 0, len(storedValues))
	for _, storedValue := range storedValues {
		ref := ConfigRef{AppID: storedValue.AppID, Name: storedValue.Name}
		plaintext, err := Decrypt(ref, key, Box{Ciphertext: storedValue.Ciphertext, Nonce: storedValue.Nonce, KeyVersion: storedValue.KeyVersion})
		if err != nil {
			return nil, err
		}
		values = append(values, decryptedEntry{ref: ref, value: string(plaintext)})
	}
	return values, nil
}

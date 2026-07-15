package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/sshaccess"
	"github.com/sshdock/sshdock/internal/store"
)

func (b *StoreBackend) SetServerGitHost(host string) error {
	baseDomain, err := domaincfg.NormalizeBaseDomain(host)
	if err != nil {
		return err
	}

	if err := b.store.SetServerConfig(context.Background(), store.ServerConfig{
		BaseDomain: baseDomain,
		GitHost:    domaincfg.ControlHost(baseDomain),
		UpdatedAt:  b.now(),
	}); err != nil {
		return fmt.Errorf("set server base domain: %w", err)
	}

	return nil
}

func (b *StoreBackend) AddSSHKey(name string, publicKey string) error {
	name = strings.TrimSpace(name)
	publicKey = strings.TrimSpace(publicKey)
	if name == "" {
		return fmt.Errorf("SSH key name is required")
	}
	if err := validatePublicKey(publicKey); err != nil {
		return err
	}
	ctx := context.Background()
	key := store.SSHKey{Name: name, PublicKey: publicKey, CreatedAt: b.now()}
	if err := b.store.UpsertSSHKey(ctx, key); err != nil {
		return fmt.Errorf("store SSH key %q: %w", name, err)
	}
	keys, err := b.store.ListSSHKeys(ctx)
	if err != nil {
		return fmt.Errorf("list SSH keys: %w", err)
	}

	if err := b.writeAuthorizedKeys(keys); err != nil {
		return err
	}

	return nil
}

func (b *StoreBackend) ListSSHKeys() ([]SSHKey, error) {
	keys, err := b.store.ListSSHKeys(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list SSH keys: %w", err)
	}
	result := make([]SSHKey, 0, len(keys))
	for _, key := range keys {
		result = append(result, SSHKey{Name: key.Name, PublicKey: key.PublicKey, CreatedAt: key.CreatedAt})
	}
	return result, nil
}

func (b *StoreBackend) RemoveSSHKey(name string) error {
	ctx := context.Background()
	if err := b.store.DeleteSSHKey(ctx, name); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("SSH key %q not found", name)
	} else if err != nil {
		return fmt.Errorf("remove SSH key %q: %w", name, err)
	}
	keys, err := b.store.ListSSHKeys(ctx)
	if err != nil {
		return fmt.Errorf("list SSH keys: %w", err)
	}
	if err := b.writeAuthorizedKeys(keys); err != nil {
		return err
	}
	return nil
}

func (b *StoreBackend) writeAuthorizedKeys(keys []store.SSHKey) error {
	if b.authorizedKeysPath != "" {
		if err := sshaccess.WriteAuthorizedKeys(b.authorizedKeysPath, sshAccessKeys(keys), b.gitReceiveCommand); err != nil {
			return fmt.Errorf("write authorized_keys: %w", err)
		}
	}
	if b.operatorAuthorizedKeysPath != "" {
		if err := sshaccess.WriteOperatorAuthorizedKeys(b.operatorAuthorizedKeysPath, sshAccessKeys(keys), b.operatorCommand); err != nil {
			return fmt.Errorf("write operator authorized_keys: %w", err)
		}
	}
	return nil
}

func (b *StoreBackend) currentGitHost(ctx context.Context) string {
	if gitHost, ok := b.persistedGitHost(ctx); ok {
		return gitHost
	}
	return b.gitHost
}

func (b *StoreBackend) persistedGitHost(ctx context.Context) (string, bool) {
	config, err := b.store.GetServerConfig(ctx)
	if err != nil {
		return "", false
	}
	if config.BaseDomain != "" {
		return domaincfg.ControlHost(config.BaseDomain), true
	}
	if config.GitHost != "" {
		return config.GitHost, true
	}
	return "", false
}

func (b *StoreBackend) currentBaseDomain(ctx context.Context) (string, bool) {
	config, err := b.store.GetServerConfig(ctx)
	if err == nil && config.BaseDomain != "" {
		return config.BaseDomain, true
	}
	return "", false
}

func sshAccessKeys(keys []store.SSHKey) []sshaccess.Key {
	result := make([]sshaccess.Key, 0, len(keys))
	for _, key := range keys {
		result = append(result, sshaccess.Key{
			Name:      key.Name,
			PublicKey: key.PublicKey,
			CreatedAt: key.CreatedAt,
		})
	}
	return result
}

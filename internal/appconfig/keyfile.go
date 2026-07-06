package appconfig

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func LoadOrCreateHostKey(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("config encryption key path is required")
	}

	key, err := os.ReadFile(path)
	if err == nil {
		return validateHostKey(path, key)
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config encryption key %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create config key directory: %w", err)
	}
	key = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate config encryption key: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("write config encryption key %s: %w", path, err)
	}
	if err := validateKeyFileMode(path); err != nil {
		return nil, err
	}
	return key, nil
}

func LoadHostKey(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("config encryption key path is required")
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config encryption key %s: %w", path, err)
	}
	return validateHostKey(path, key)
}

func validateHostKey(path string, key []byte) ([]byte, error) {
	if err := validateKeyFileMode(path); err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("config encryption key %s must be 32 bytes, got %d", path, len(key))
	}
	return key, nil
}

func validateKeyFileMode(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config encryption key %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config encryption key %s must be a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("config encryption key %s must not be group- or world-readable", path)
	}
	return nil
}

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
	// Publish a complete key without replacing a concurrent creator's key.
	// O_EXCL on the final path alone would expose an empty/partial key to readers.
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-key-*")
	if err != nil {
		return nil, fmt.Errorf("stage config encryption key %s: %w", path, err)
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if _, err := temporary.Write(key); err != nil {
		return nil, fmt.Errorf("write config encryption key %s: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		return nil, fmt.Errorf("sync config encryption key %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("close staged config encryption key: %w", err)
	}
	if err := os.Link(temporary.Name(), path); os.IsExist(err) {
		return LoadHostKey(path)
	} else if err != nil {
		return nil, fmt.Errorf("install config encryption key %s: %w", path, err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("open config key directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return nil, fmt.Errorf("sync config key directory: %w", err)
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

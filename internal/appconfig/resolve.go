package appconfig

import (
	"context"
	"fmt"
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
	return s.ResolveEnv(ctx, appID)
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

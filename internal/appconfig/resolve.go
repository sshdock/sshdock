package appconfig

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type ResolveRequest struct {
	AppID      string
	ProjectDir string
}

type decryptedEntry struct {
	ref   ConfigRef
	value string
}

func (s *Service) ResolveEnv(ctx context.Context, request ResolveRequest) (map[string]string, error) {
	values, err := s.decryptedValues(ctx, request.AppID)
	if err != nil {
		return nil, err
	}
	env := make(map[string]string)
	byIdentity := make(map[string]string, len(values))
	for _, value := range values {
		byIdentity[value.ref.Scope+"\x00"+value.ref.Name] = value.value
		if value.ref.Scope == "" {
			if isReservedConfigName(value.ref.Name) {
				return nil, reservedConfigNameError(value.ref.Name)
			}
			env[value.ref.Name] = value.value
		}
	}

	manifest, err := LoadManifest(request.ProjectDir)
	if err != nil {
		return nil, err
	}
	var missing []RequiredKey
	for _, required := range manifest.Required {
		value, found := byIdentity[required.identity()]
		if !found {
			missing = append(missing, required)
			continue
		}
		if required.Scope == "" {
			continue
		}
		if isReservedConfigName(required.Name) {
			return nil, reservedConfigNameError(required.Name)
		}
		if _, exists := env[required.Name]; exists {
			return nil, fmt.Errorf("config key %s is declared more than once for Compose environment resolution", required.Name)
		}
		env[required.Name] = value
	}
	if len(missing) > 0 {
		return nil, missingRequiredError(request.AppID, s.recoveryHost, missing)
	}
	return env, nil
}

func (s *Service) ResolveAppConfig(ctx context.Context, appID string, projectDir string) (map[string]string, error) {
	return s.ResolveEnv(ctx, ResolveRequest{AppID: appID, ProjectDir: projectDir})
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
		ref := ConfigRef{AppID: storedValue.AppID, Name: storedValue.Name, Scope: storedValue.Scope}
		plaintext, err := Decrypt(ref, key, Box{Ciphertext: storedValue.Ciphertext, Nonce: storedValue.Nonce, KeyVersion: storedValue.KeyVersion})
		if err != nil {
			return nil, err
		}
		values = append(values, decryptedEntry{ref: ref, value: string(plaintext)})
	}
	return values, nil
}

func missingRequiredError(appID string, host string, missing []RequiredKey) error {
	names := make([]string, 0, len(missing))
	commands := make([]string, 0, len(missing))
	for _, key := range missing {
		names = append(names, key.display())
		command := fmt.Sprintf("ssh dashboard@%s config set %s %s", host, appID, key.Name)
		if key.Scope != "" {
			command += " --scope " + key.Scope
		}
		commands = append(commands, command)
	}
	sort.Strings(names)
	sort.Strings(commands)
	return fmt.Errorf("missing required config for %s: %s\nset missing values:\n  %s", appID, strings.Join(names, ", "), strings.Join(commands, "\n  "))
}

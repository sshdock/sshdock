package appconfig

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

type configStore interface {
	GetApp(ctx context.Context, id string) (appmodel.App, error)
	UpsertAppConfigValue(ctx context.Context, value store.AppConfigValue) error
	GetAppConfigValue(ctx context.Context, ref store.AppConfigRef) (store.AppConfigValue, error)
	ListAppConfigValues(ctx context.Context, appID string) ([]store.AppConfigValue, error)
	DeleteAppConfigValue(ctx context.Context, ref store.AppConfigRef) error
}

type Service struct {
	store        configStore
	keyPath      string
	now          func() time.Time
	recoveryHost string
}

type ServiceOption func(*Service)

type SetRequest struct {
	AppID     string
	Name      string
	Scope     string
	Value     []byte
	MutatedBy string
}

type ImportRequest struct {
	AppID     string
	Scope     string
	Input     io.Reader
	MutatedBy string
}

type ResolveRequest struct {
	AppID      string
	ProjectDir string
}

type Entry struct {
	Name          string
	Scope         string
	Status        string
	RedactedValue string
	Value         string
	UpdatedAt     time.Time
	MutatedBy     string
}

func NewService(persistentStore configStore, keyPath string, options ...ServiceOption) *Service {
	service := &Service{
		store:   persistentStore,
		keyPath: keyPath,
		now: func() time.Time {
			return time.Now().UTC()
		},
		recoveryHost: "server",
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func WithClock(clock func() time.Time) ServiceOption {
	return func(service *Service) {
		service.now = clock
	}
}

func WithRecoveryHost(host string) ServiceOption {
	return func(service *Service) {
		if strings.TrimSpace(host) != "" {
			service.recoveryHost = strings.TrimSpace(host)
		}
	}
}

func (s *Service) Set(ctx context.Context, request SetRequest) error {
	ref, err := validateConfigRef(ConfigRef{AppID: request.AppID, Name: request.Name, Scope: request.Scope})
	if err != nil {
		return err
	}
	if err := s.requireApp(ctx, ref.AppID); err != nil {
		return err
	}
	key, err := LoadOrCreateHostKey(s.keyPath)
	if err != nil {
		return err
	}
	box, err := Encrypt(ref, key, request.Value)
	if err != nil {
		return err
	}

	now := s.now()
	createdAt := now
	if existing, err := s.store.GetAppConfigValue(ctx, storeRef(ref)); err == nil {
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("load existing config value %s: %w", ref.display(), err)
	}
	mutatedBy := strings.TrimSpace(request.MutatedBy)
	if mutatedBy == "" {
		mutatedBy = "dashboard"
	}

	return s.store.UpsertAppConfigValue(ctx, store.AppConfigValue{
		AppID:      ref.AppID,
		Name:       ref.Name,
		Scope:      ref.Scope,
		Ciphertext: box.Ciphertext,
		Nonce:      box.Nonce,
		KeyVersion: box.KeyVersion,
		CreatedAt:  createdAt,
		UpdatedAt:  now,
		MutatedBy:  mutatedBy,
	})
}

func (s *Service) Import(ctx context.Context, request ImportRequest) error {
	if request.Input == nil {
		request.Input = strings.NewReader("")
	}
	scanner := bufio.NewScanner(request.Input)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("config import line %d must be KEY=VALUE", lineNumber)
		}
		if err := s.Set(ctx, SetRequest{
			AppID:     request.AppID,
			Name:      strings.TrimSpace(name),
			Scope:     request.Scope,
			Value:     []byte(value),
			MutatedBy: request.MutatedBy,
		}); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Service) List(ctx context.Context, appID string) ([]Entry, error) {
	if err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	values, err := s.store.ListAppConfigValues(ctx, appID)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(values))
	for _, value := range values {
		entries = append(entries, Entry{
			Name:          value.Name,
			Scope:         value.Scope,
			Status:        "set",
			RedactedValue: "<redacted>",
			UpdatedAt:     value.UpdatedAt,
			MutatedBy:     value.MutatedBy,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Scope == entries[j].Scope {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Scope < entries[j].Scope
	})
	return entries, nil
}

func (s *Service) Reveal(ctx context.Context, ref ConfigRef) (string, error) {
	ref, err := validateConfigRef(ref)
	if err != nil {
		return "", err
	}
	if err := s.requireApp(ctx, ref.AppID); err != nil {
		return "", err
	}
	value, err := s.store.GetAppConfigValue(ctx, storeRef(ref))
	if err != nil {
		return "", err
	}
	key, err := LoadHostKey(s.keyPath)
	if err != nil {
		return "", err
	}
	plaintext, err := Decrypt(ref, key, Box{
		Ciphertext: value.Ciphertext,
		Nonce:      value.Nonce,
		KeyVersion: value.KeyVersion,
	})
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (s *Service) Unset(ctx context.Context, ref ConfigRef) error {
	ref, err := validateConfigRef(ref)
	if err != nil {
		return err
	}
	if err := s.requireApp(ctx, ref.AppID); err != nil {
		return err
	}
	return s.store.DeleteAppConfigValue(ctx, storeRef(ref))
}

func (s *Service) ResolveEnv(ctx context.Context, request ResolveRequest) (map[string]string, error) {
	manifest, err := LoadManifest(request.ProjectDir)
	if err != nil {
		return nil, err
	}
	if len(manifest.Required) == 0 {
		return nil, nil
	}
	if err := s.requireApp(ctx, request.AppID); err != nil {
		return nil, err
	}

	env := map[string]string{}
	var missing []RequiredKey
	for _, required := range manifest.Required {
		ref := ConfigRef{AppID: request.AppID, Name: required.Name, Scope: required.Scope}
		value, err := s.Reveal(ctx, ref)
		if errors.Is(err, store.ErrNotFound) {
			missing = append(missing, required)
			continue
		}
		if err != nil {
			return nil, err
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

func (s *Service) requireApp(ctx context.Context, appID string) error {
	if strings.TrimSpace(appID) == "" {
		return fmt.Errorf("app name is required")
	}
	if _, err := s.store.GetApp(ctx, appID); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("app %q not found", appID)
	} else if err != nil {
		return fmt.Errorf("get app %q: %w", appID, err)
	}
	return nil
}

func validateConfigRef(ref ConfigRef) (ConfigRef, error) {
	ref.AppID = strings.TrimSpace(ref.AppID)
	ref.Name = strings.TrimSpace(ref.Name)
	ref.Scope = strings.TrimSpace(ref.Scope)
	if ref.AppID == "" {
		return ConfigRef{}, fmt.Errorf("app name is required")
	}
	key, err := validateRequiredKey(RequiredKey{Name: ref.Name, Scope: ref.Scope})
	if err != nil {
		return ConfigRef{}, err
	}
	ref.Name = key.Name
	ref.Scope = key.Scope
	return ref, nil
}

func storeRef(ref ConfigRef) store.AppConfigRef {
	return store.AppConfigRef{AppID: ref.AppID, Name: ref.Name, Scope: ref.Scope}
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

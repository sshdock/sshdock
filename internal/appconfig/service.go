package appconfig

import (
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
	SetAppConfigValue(ctx context.Context, value store.AppConfigValue, event appmodel.Event) error
	GetAppConfigValue(ctx context.Context, ref store.AppConfigRef) (store.AppConfigValue, error)
	ListAppConfigValues(ctx context.Context, appID string) ([]store.AppConfigValue, error)
	DeleteAppConfigValue(ctx context.Context, ref store.AppConfigRef, event appmodel.Event) error
}

type Service struct {
	store   configStore
	keyPath string
	now     func() time.Time
}

type ServiceOption func(*Service)

type SetRequest struct {
	AppID     string
	Name      string
	Value     []byte
	MutatedBy string
}

type ImportRequest struct {
	AppID     string
	Input     io.Reader
	MutatedBy string
}

type Entry struct {
	Name          string
	Status        string
	RedactedValue string
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

func (s *Service) Set(ctx context.Context, request SetRequest) error {
	ref, err := validateConfigMutationRef(ConfigRef{AppID: request.AppID, Name: request.Name})
	if err != nil {
		return err
	}
	if err := s.requireApp(ctx, ref.AppID); err != nil {
		return err
	}
	event, err := s.mutationEvent(ref, "set")
	if err != nil {
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

	mutatedBy := strings.TrimSpace(request.MutatedBy)
	if mutatedBy == "" {
		mutatedBy = "dashboard"
	}

	return s.store.SetAppConfigValue(ctx, store.AppConfigValue{
		AppID:      ref.AppID,
		Name:       ref.Name,
		Ciphertext: box.Ciphertext,
		Nonce:      box.Nonce,
		KeyVersion: box.KeyVersion,
		CreatedAt:  event.CreatedAt,
		UpdatedAt:  event.CreatedAt,
		MutatedBy:  mutatedBy,
	}, event)
}

func (s *Service) Import(ctx context.Context, request ImportRequest) (int, error) {
	request.AppID = strings.TrimSpace(request.AppID)
	if err := s.requireApp(ctx, request.AppID); err != nil {
		return 0, err
	}
	return ImportLines(request.Input, func(name string, value []byte) error {
		return s.Set(ctx, SetRequest{AppID: request.AppID, Name: name, Value: value, MutatedBy: request.MutatedBy})
	})
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
			Status:        "set",
			RedactedValue: "<redacted>",
			UpdatedAt:     value.UpdatedAt,
			MutatedBy:     value.MutatedBy,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
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
	event, err := s.mutationEvent(ref, "unset")
	if err != nil {
		return err
	}
	return s.store.DeleteAppConfigValue(ctx, storeRef(ref), event)
}

func (s *Service) mutationEvent(ref ConfigRef, operation string) (appmodel.Event, error) {
	id, err := appmodel.NewDeploymentID()
	if err != nil {
		return appmodel.Event{}, fmt.Errorf("create config mutation event ID: %w", err)
	}
	eventType := "config." + operation
	return appmodel.Event{
		ID: "evt_" + id + "_config_" + operation, AppID: ref.AppID,
		Type: eventType, Message: "Config " + operation + " for " + ref.Name, CreatedAt: s.now(),
	}, nil
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

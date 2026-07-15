package cli

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

type failingEventUpdateStore struct {
	store.Store
}

func (failingEventUpdateStore) UpdateEventMessage(context.Context, string, string) error {
	return errors.New("database is read-only")
}

func TestStoreBackendRemoveRetainsStartedAndSucceededAuditEvents(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := t.TempDir()
	currentTime := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, currentTime)
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		AppsDir:        appsDir,
		RecoveryRunner: &compose.FakeRunner{},
		Router:         &fakeRoutePublisher{},
		Now: func() time.Time {
			value := currentTime
			currentTime = currentTime.Add(time.Second)
			return value
		},
	})

	// When
	if err := backend.RemoveApp("my-app"); err != nil {
		t.Fatalf("RemoveApp: %v", err)
	}

	// Then
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if len(events) != 2 || events[0].Type != "remove.started" || events[1].Type != "remove.succeeded" {
		t.Fatalf("events = %#v, want remove started and succeeded", events)
	}
	auditEvents, err := backend.ListEvents("my-app")
	if err != nil {
		t.Fatalf("ListEvents after removal: %v", err)
	}
	if len(auditEvents) != 2 {
		t.Fatalf("ListEvents after removal = %#v, want retained audit events", auditEvents)
	}
}

func TestStoreBackendRemoveFailureRetainsActionableAuditEvent(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := t.TempDir()
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	failure := errors.New("compose down failed")
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		AppsDir:        appsDir,
		RecoveryRunner: &compose.FakeRunner{RemoveErr: failure},
		Now:            func() time.Time { return now },
	})

	// When
	err := backend.RemoveApp("my-app")

	// Then
	if !errors.Is(err, failure) {
		t.Fatalf("RemoveApp error = %v, want wrapped %v", err, failure)
	}
	events, listErr := sqlite.ListEventsByApp(ctx, "my-app")
	if listErr != nil {
		t.Fatalf("ListEventsByApp: %v", listErr)
	}
	if len(events) != 2 || events[0].Type != "remove.started" || events[1].Type != "remove.failed" {
		t.Fatalf("events = %#v, want remove started and failed", events)
	}
	if !strings.Contains(events[1].Message, "sudo sshdock apps remove my-app --force") {
		t.Fatalf("remove.failed message = %q, want retry guidance", events[1].Message)
	}
}

func TestStoreBackendRemovePersistsRedactedEventsBeforeDeletingConfig(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := t.TempDir()
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	secret := "postgres://secret"
	configService := appconfig.NewService(sqlite, filepath.Join(t.TempDir(), "config.key"))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte(secret)}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	if err := sqlite.CreateEvent(ctx, app.Event{ID: "evt_secret", AppID: "my-app", Type: "deploy.failed", Message: "failed for " + secret, CreatedAt: now}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	runner := &compose.FakeRunner{}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{AppsDir: appsDir, ConfigManager: configService, RecoveryRunner: runner, Router: &fakeRoutePublisher{}})

	if err := backend.RemoveApp("my-app"); err != nil {
		t.Fatalf("RemoveApp: %v", err)
	}
	events, err := backend.ListEvents("my-app")
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	for _, event := range events {
		if strings.Contains(event.Message, secret) {
			t.Fatalf("retained event leaked deleted config value: %#v", event)
		}
	}
	redacted := false
	for _, event := range events {
		if event.Type == "deploy.failed" && strings.Contains(event.Message, "<redacted>") {
			redacted = true
		}
	}
	if !redacted {
		t.Fatalf("retained event was not persistently redacted: %#v", events)
	}
	if len(runner.RemoveRequests) != 1 || runner.RemoveRequests[0].Env["DATABASE_URL"] != secret {
		t.Fatalf("remove requests = %#v, want stored config environment", runner.RemoveRequests)
	}
}

func TestStoreBackendRemoveCanResumeAfterPostDeleteRouteFailure(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := t.TempDir()
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	routeFailure := errors.New("Caddy unavailable")
	router := &fakeRoutePublisher{Err: routeFailure}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{AppsDir: appsDir, Router: router})

	if err := backend.RemoveApp("my-app"); !errors.Is(err, routeFailure) {
		t.Fatalf("RemoveApp error = %v, want wrapped %v", err, routeFailure)
	}
	router.Err = nil
	if err := backend.RemoveApp("my-app"); err != nil {
		t.Fatalf("retry RemoveApp: %v", err)
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if events[len(events)-1].Type != "remove.succeeded" {
		t.Fatalf("last event = %#v, want remove.succeeded", events[len(events)-1])
	}
}

func TestStoreBackendRemoveScrubsEventsBeforeDestructiveWork(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := t.TempDir()
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	secret := "postgres://secret"
	configService := appconfig.NewService(sqlite, filepath.Join(t.TempDir(), "config.key"))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte(secret)}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	if err := sqlite.CreateEvent(ctx, app.Event{ID: "evt_secret", AppID: "my-app", Type: "deploy.failed", Message: "failed for " + secret, CreatedAt: now}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	runner := &compose.FakeRunner{}
	backend := NewStoreBackend(failingEventUpdateStore{Store: sqlite}, StoreBackendConfig{AppsDir: appsDir, ConfigManager: configService, RecoveryRunner: runner})

	err := backend.RemoveApp("my-app")
	if err == nil || !strings.Contains(err.Error(), "redact retained events") {
		t.Fatalf("RemoveApp error = %v, want redaction failure", err)
	}
	if len(runner.RemoveRequests) != 0 {
		t.Fatalf("Compose removal ran before event redaction: %#v", runner.RemoveRequests)
	}
	if _, err := sqlite.GetApp(ctx, "my-app"); err != nil {
		t.Fatalf("app state changed after redaction failure: %v", err)
	}
}

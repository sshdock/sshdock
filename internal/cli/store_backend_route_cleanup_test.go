package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/store"
)

type failingRouteFailureClearStore struct {
	store.Store
}

func (failingRouteFailureClearStore) ClearRouteApplyFailures(context.Context) error {
	return errors.New("database is read-only")
}

func TestStoreBackendDoesNotReportSuccessfulReloadAsFailedWhenCleanupFails(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	createDomainTestApp(t, sqlite, now)
	backend := NewStoreBackend(failingRouteFailureClearStore{Store: sqlite}, StoreBackendConfig{
		Router: &fakeRoutePublisher{},
		Now:    func() time.Time { return now },
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := NewRunner(backend, "dev").Run(
		[]string{"domains", "attach", "my-app", "web", "my-app.example.com", "--port", "3000"},
		&stdout,
		&stderr,
	)
	if code != 1 {
		t.Fatalf("domains attach exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Caddy routes reloaded, but clear resolved route failures: database is read-only") {
		t.Fatalf("domains attach stderr = %q", stderr.String())
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if len(events) != 2 || events[0].Type != "domain.attached" || events[1].Type != "router.reloaded" {
		t.Fatalf("events = %#v, want domain.attached and router.reloaded", events)
	}
}

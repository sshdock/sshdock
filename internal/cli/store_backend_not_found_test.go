package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestStoreBackendReadOnlyNotFoundPreservesFriendlyMessage(t *testing.T) {
	sqlite := newStoreBackendTestStore(t, context.Background())
	backend := NewStoreBackend(sqlite, StoreBackendConfig{})
	for name, read := range map[string]func() error{
		"GetApp":          func() error { _, err := backend.GetApp("missing"); return err },
		"AppHealth":       func() error { _, err := backend.AppHealth("missing"); return err },
		"ListDeployments": func() error { _, err := backend.ListDeployments("missing"); return err },
		"ListReleases":    func() error { _, err := backend.ListReleases("missing"); return err },
		"ListEvents":      func() error { _, err := backend.ListEvents("missing"); return err },
		"ListDomains":     func() error { _, err := backend.ListDomains("missing"); return err },
		"CheckDomains":    func() error { _, err := backend.CheckDomains("missing"); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := read()
			if !errors.Is(err, ErrNotFound) || err.Error() != `app "missing" not found` {
				t.Fatalf("read error = %v, want friendly ErrNotFound", err)
			}
		})
	}
}

func TestStoreBackendRemovedAppEventsRetainIdentity(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	if err := sqlite.CreateEvent(ctx, app.Event{ID: "evt_removed", AppID: "removed", Type: "remove.succeeded", Message: "removed app", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	events, err := NewStoreBackend(sqlite, StoreBackendConfig{}).ListEvents("removed")
	if err != nil || len(events) != 1 || events[0].ID != "evt_removed" {
		t.Fatalf("removed app events = %#v, error = %v", events, err)
	}
}

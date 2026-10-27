package cli

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
)

func TestStoreBackendConfigMutationsDelegateAuditAndPartialImportToOwner(t *testing.T) {
	ctx := context.Background()
	db := newStoreBackendTestStore(t, ctx)
	if err := db.CreateApp(ctx, app.App{ID: "my-app", Name: "my-app"}); err != nil {
		t.Fatal(err)
	}
	manager := appconfig.NewService(db, filepath.Join(t.TempDir(), "config.key"))
	backend := NewStoreBackend(db, StoreBackendConfig{
		ConfigManager: manager,
		NewDeploymentID: func() (string, error) {
			return "", errors.New("deployment ID generation must not own config audit")
		},
	})
	if err := backend.SetConfig("my-app", "FIRST", []byte("first-secret")); err != nil {
		t.Fatal(err)
	}
	count, err := backend.ImportConfig("my-app", strings.NewReader("# comment\nSECOND=second=secret\nTHIRD=third-secret\ninvalid\nFOURTH=never-written\n"))
	if count != 2 || err == nil || !strings.Contains(err.Error(), "line 4") {
		t.Fatalf("import = %d, %v; want two persisted lines and line 4 error", count, err)
	}
	if err := backend.UnsetConfig("my-app", "FIRST"); err != nil {
		t.Fatal(err)
	}
	entries, err := backend.ListConfig("my-app")
	if err != nil || len(entries) != 2 || entries[0].Name != "SECOND" || entries[1].Name != "THIRD" {
		t.Fatalf("config entries = %#v, %v", entries, err)
	}
	if value, err := backend.GetConfig("my-app", "SECOND"); err != nil || value != "second=secret" {
		t.Fatalf("reveal = %q, %v", value, err)
	}
	events, err := db.ListEventsByApp(ctx, "my-app")
	if err != nil || len(events) != 4 {
		t.Fatalf("config mutation events = %#v, %v; want one event per persisted mutation", events, err)
	}
	counts := map[string]int{}
	for _, event := range events {
		counts[event.Type]++
		if strings.Contains(event.Message, "secret") {
			t.Fatalf("config event exposed plaintext: %#v", event)
		}
	}
	if counts["config.set"] != 3 || counts["config.unset"] != 1 {
		t.Fatalf("event counts = %#v", counts)
	}
}

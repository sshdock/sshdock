package store

import (
	"context"
	"errors"
	"github.com/sshdock/sshdock/internal/app"
	"testing"
	"time"
)

func TestAttachDomainCannotRecreateRoutesAfterAppRemoval(t *testing.T) {
	for _, attachFirst := range []bool{false, true} {
		name := "remove before attach"
		if attachFirst {
			name = "attach before remove"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := newTestStore(t, ctx)
			now := time.Now()
			if err := db.CreateApp(ctx, app.App{ID: "app", Name: "app", Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			route := app.Domain{ID: "domain", AppID: "app", DomainName: "example.com", ServiceName: "web", Port: 3000, CreatedAt: now, UpdatedAt: now}
			if attachFirst {
				if err := db.AttachDomain(ctx, route); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.DeleteApp(ctx, "app"); err != nil {
				t.Fatal(err)
			}
			if err := db.AttachDomain(ctx, route); !errors.Is(err, ErrNotFound) {
				t.Fatalf("attach after removal = %v, want app not found", err)
			}
			domains, err := db.ListDomains(ctx)
			if err != nil || len(domains) != 0 {
				t.Fatalf("orphan domains = %#v, %v", domains, err)
			}
		})
	}
}

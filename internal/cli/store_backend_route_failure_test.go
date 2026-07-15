package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/router"
)

func TestStoreBackendDomainsCheckReportsFailedAttach(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	createDomainTestApp(t, sqlite, now)
	routePublisher := &fakeRoutePublisher{Err: errors.New("caddy reload failed")}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		Router: routePublisher,
		Now:    func() time.Time { return now },
	})
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runner.Run([]string{"domains", "attach", "my-app", "web", "my-app.example.com", "--port", "3000"}, &stdout, &stderr); code != 1 {
		t.Fatalf("domains attach exit code = %d, want 1", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"domains", "check", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains check exit code = %d, stderr = %q", code, stderr.String())
	}
	want := "my-app.example.com\tweb\t3000\ttrue\tfailed\tattach failed to apply: caddy reload failed"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("domains check stdout missing %q:\n%s", want, stdout.String())
	}
}

func TestStoreBackendDomainsCheckReportsFailedDetachStillActiveInCaddy(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	createDomainTestApp(t, sqlite, now)
	domain := app.Domain{
		ID: "dom_my_app_example_com", AppID: "my-app", ServiceName: "web",
		DomainName: "my-app.example.com", Port: 3000, HTTPS: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := sqlite.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	activeRoute := router.Route{
		AppID: "my-app", ServiceName: "web", DomainName: domain.DomainName,
		Port: domain.Port, HTTPS: domain.HTTPS,
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		Router: &fakeRoutePublisher{
			StoredRoutes: []router.Route{activeRoute},
			Err:          errors.New("caddy reload failed"),
		},
		Now: func() time.Time { return now },
	})
	runner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runner.Run([]string{"domains", "detach", "my-app", domain.DomainName}, &stdout, &stderr); code != 1 {
		t.Fatalf("domains detach exit code = %d, want 1", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"domains", "check", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains check exit code = %d, stderr = %q", code, stderr.String())
	}
	want := "my-app.example.com\tweb\t3000\ttrue\tfailed\tdetach failed to apply: caddy reload failed"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("domains check stdout missing %q:\n%s", want, stdout.String())
	}
}

func TestStoreBackendDomainAttachRecordsEveryReloadFailure(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	createDomainTestApp(t, sqlite, now)
	routePublisher := &fakeRoutePublisher{Err: errors.New("caddy reload failed")}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		Router: routePublisher,
		Now:    func() time.Time { return now },
	})
	runner := NewRunner(backend, "dev")

	for range 2 {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if code := runner.Run([]string{"domains", "attach", "my-app", "web", "my-app.example.com", "--port", "3000"}, &stdout, &stderr); code != 1 {
			t.Fatalf("domains attach exit code = %d, want 1", code)
		}
	}
	events, err := sqlite.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	var attached int
	var reloadFailed int
	for _, event := range events {
		switch event.Type {
		case "domain.attached":
			attached++
		case "router.reload_failed":
			reloadFailed++
		}
	}
	if attached != 2 || reloadFailed != 2 {
		t.Fatalf("event counts attached=%d reload_failed=%d, want 2 each", attached, reloadFailed)
	}
	routePublisher.Err = nil
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runner.Run([]string{"domains", "attach", "my-app", "web", "my-app.example.com", "--port", "3000"}, &stdout, &stderr); code != 0 {
		t.Fatalf("successful domains attach exit code = %d, stderr = %q", code, stderr.String())
	}
	failures, err := sqlite.ListRouteApplyFailuresByApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListRouteApplyFailuresByApp: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("route apply failures after successful sync = %#v, want none", failures)
	}
}

func TestStoreBackendDomainsCheckReportsHTTPSMismatch(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	createDomainTestApp(t, sqlite, now)
	domain := app.Domain{
		ID: "dom_my_app_example_com", AppID: "my-app", ServiceName: "web",
		DomainName: "my-app.example.com", Port: 3000, HTTPS: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := sqlite.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		Router: &fakeRoutePublisher{StoredRoutes: []router.Route{{
			DomainName: domain.DomainName,
			Port:       domain.Port,
			HTTPS:      false,
		}}},
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := NewRunner(backend, "dev").Run([]string{"domains", "check", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains check exit code = %d, stderr = %q", code, stderr.String())
	}
	want := "my-app.example.com\tweb\t3000\ttrue\tmismatch\trouter route differs"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("domains check stdout missing %q:\n%s", want, stdout.String())
	}
}

func TestStoreBackendDomainsCheckMatchesExplicitHTTPSAddress(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	createDomainTestApp(t, sqlite, now)
	domain := app.Domain{
		ID: "dom_my_app_example_com", AppID: "my-app", ServiceName: "web",
		DomainName: "https://my-app.example.com", Port: 3000, HTTPS: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := sqlite.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		Router: &fakeRoutePublisher{StoredRoutes: []router.Route{{
			DomainName: "my-app.example.com",
			Port:       domain.Port,
			HTTPS:      true,
		}}},
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := NewRunner(backend, "dev").Run([]string{"domains", "check", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains check exit code = %d, stderr = %q", code, stderr.String())
	}
	want := "https://my-app.example.com\tweb\t3000\ttrue\tok\tactive Caddy route matches"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("domains check stdout missing %q:\n%s", want, stdout.String())
	}
}

func createDomainTestApp(t *testing.T, sqlite interface {
	CreateApp(context.Context, app.App) error
}, now time.Time) {
	t.Helper()
	model := app.App{ID: "my-app", Name: "my-app", NodeID: "node-a", Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}
	if err := sqlite.CreateApp(context.Background(), model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
}

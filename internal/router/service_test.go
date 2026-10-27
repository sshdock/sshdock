package router

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestCanonicalAddress(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
		https bool
	}{
		{"Example.COM", "example.com", true},
		{"https://Example.COM:443", "example.com", true},
		{"http://Example.COM:80", "http://example.com", false},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080", false},
		{"example.com:8443", "https://example.com:8443", true},
		{"example.com:80", "http://example.com", false},
		{"https://example.com:80", "https://example.com:80", true},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, https, err := canonicalAddress(test.input)
			if err != nil || got != test.want || https != test.https {
				t.Fatalf("canonicalAddress = %q, %t, %v; want %q, %t", got, https, err, test.want, test.https)
			}
		})
	}
	for _, input := range []string{"", " example.com", "example.com\n", "example.com { respond injected }", "example.com\x1b", "ftp://example.com", "https://user@example.com", "example.com/path", "example.com?", "example.com#", "example.com:", "example.com:0", "example.com:65536", "bad_host.example.com"} {
		t.Run(input, func(t *testing.T) {
			if _, _, err := canonicalAddress(input); err == nil {
				t.Fatalf("accepted unsafe site address %q", input)
			}
		})
	}
}

func TestServiceInitialRoutePersistsIntentAndDoesNotReplaceExistingRoute(t *testing.T) {
	ctx := context.Background()
	db, adapter, service := newInitialRouteService(t, "my-app", "example.com")
	request := InitialRouteRequest{AppID: "my-app", DeploymentID: "dep_initial", Target: &Route{ServiceName: "web", Port: 3100}}
	if reason, err := service.EnsureInitial(ctx, request); err != nil || reason != "" {
		t.Fatalf("EnsureInitial = %q, %v", reason, err)
	}
	domains, err := db.ListDomainsByApp(ctx, "my-app")
	if err != nil || len(domains) != 1 || domains[0].DomainName != "my-app.example.com" || domains[0].Port != 3100 || !domains[0].HTTPS {
		t.Fatalf("initial domain = %#v, %v", domains, err)
	}
	for _, target := range []*Route{nil, {ServiceName: "api", Port: 4100}} {
		request.Target, request.NoTargetReason = target, "ambiguous later Compose model"
		if reason, err := service.EnsureInitial(ctx, request); err != nil || reason != "" {
			t.Fatalf("existing route became skipped or failed: %q, %v", reason, err)
		}
	}
	routes, err := adapter.Routes(ctx)
	if err != nil || len(routes) != 1 || routes[0].ServiceName != "web" || routes[0].Port != 3100 {
		t.Fatalf("initial route was replaced: %#v, %v", routes, err)
	}
	events, err := db.ListEventsByApp(ctx, "my-app")
	if err != nil || len(events) != 2 || events[0].Type != "route.auto_attached" || events[1].Type != "router.reloaded" {
		t.Fatalf("initial route audit = %#v, %v", events, err)
	}
}

func TestServiceInitialRouteReturnsOnlyUnroutedSkipReasons(t *testing.T) {
	for _, test := range []struct {
		name   string
		appID  string
		base   string
		target *Route
		reason string
		skip   string
	}{
		{name: "inference", appID: "my-app", base: "example.com", reason: "ambiguous published ports", skip: "ambiguous published ports"},
		{name: "empty inference reason", appID: "my-app", base: "example.com", skip: "no safe host-published"},
		{name: "no base domain", appID: "my-app", target: &Route{ServiceName: "web", Port: 3100}},
		{name: "invalid app host", appID: "My_App", base: "example.com", target: &Route{ServiceName: "web", Port: 3100}, skip: "not a normalized DNS label"},
		{name: "invalid inferred port", appID: "my-app", base: "example.com", target: &Route{ServiceName: "web", Port: 0}, skip: "1 and 65535"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db, _, service := newInitialRouteService(t, test.appID, test.base)
			reason, err := service.EnsureInitial(ctx, InitialRouteRequest{AppID: test.appID, DeploymentID: "dep_skip", Target: test.target, NoTargetReason: test.reason})
			if err != nil || (test.skip == "" && reason != "") || (test.skip != "" && !strings.Contains(reason, test.skip)) {
				t.Fatalf("EnsureInitial = %q, %v; want skip containing %q", reason, err, test.skip)
			}
			domains, err := db.ListDomains(ctx)
			if err != nil || len(domains) != 0 {
				t.Fatalf("skipped route mutated intent = %#v, %v", domains, err)
			}
			events, err := db.ListEventsByApp(ctx, test.appID)
			if err != nil || len(events) != 0 {
				t.Fatalf("routing owner duplicated deployment skip audit = %#v, %v", events, err)
			}
		})
	}
}

func TestServiceInitialApplyFailureStaysRecoverableAndReconcileWorksAfterRemoval(t *testing.T) {
	ctx := context.Background()
	db, adapter, service := newInitialRouteService(t, "my-app", "example.com")
	cause := errors.New("Caddy unavailable")
	adapter.SyncErr = cause
	reason, err := service.EnsureInitial(ctx, InitialRouteRequest{AppID: "my-app", DeploymentID: "dep_initial", Target: &Route{ServiceName: "web", Port: 3100}})
	if err != nil || reason != "" {
		t.Fatalf("recoverable initial Caddy failure failed deploy: %q, %v", reason, err)
	}
	failures, err := db.ListRouteApplyFailuresByApp(ctx, "my-app")
	if err != nil || len(failures) != 1 || failures[0].Detail != cause.Error() {
		t.Fatalf("initial apply failure = %#v, %v", failures, err)
	}
	if err := service.Reconcile(ctx); !errors.Is(err, cause) {
		t.Fatalf("Reconcile hid Caddy failure: %v", err)
	}
	if err := db.DeleteApp(ctx, "my-app"); err != nil {
		t.Fatal(err)
	}
	adapter.SyncErr = nil
	if err := service.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile required deleted app: %v", err)
	}
	failures, err = db.ListRouteApplyFailuresByApp(ctx, "my-app")
	if err != nil || len(failures) != 0 {
		t.Fatalf("successful rebuild retained resolved failure = %#v, %v", failures, err)
	}
	routes, err := adapter.Routes(ctx)
	if err != nil || len(routes) != 0 {
		t.Fatalf("removed app route remained active: %#v, %v", routes, err)
	}
}

func TestServiceRejectsDuplicateAliasesUntilDetached(t *testing.T) {
	ctx := context.Background()
	db, _, service := newInitialRouteService(t, "my-app", "example.com")
	now := time.Now().UTC()
	for _, address := range []string{"example.com", "https://example.com"} {
		if err := db.AttachDomain(ctx, app.Domain{ID: address, AppID: "my-app", ServiceName: "web", DomainName: address, Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Attach(ctx, AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3100}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("ambiguous aliases were silently overwritten: %v", err)
	}
	if err := service.Detach(ctx, "my-app", "https://EXAMPLE.COM:443"); err != nil {
		t.Fatal(err)
	}
	domains, err := db.ListDomains(ctx)
	if err != nil || len(domains) != 0 {
		t.Fatalf("detach left public address aliases = %#v, %v", domains, err)
	}
}

func newInitialRouteService(t *testing.T, appID, baseDomain string) (*store.SQLiteStore, *FakeRouter, *Service) {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := db.CreateApp(ctx, app.App{ID: appID, Name: appID, NodeID: "local", Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if baseDomain != "" {
		if err := db.SetServerConfig(ctx, store.ServerConfig{BaseDomain: baseDomain, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	adapter := NewFakeRouter()
	return db, adapter, NewService(ServiceConfig{Store: db, Router: adapter, LocksDir: t.TempDir(), Now: func() time.Time { return now }})
}

func TestServiceRecordsApplyFailureAfterCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, _, service := newInitialRouteService(t, "my-app", "example.com")
	service.router = cancelingRouteApplier{cancel: cancel}
	err := service.Attach(ctx, AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: "my-app.example.com", Port: 3100})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Attach = %v, want canceled apply", err)
	}
	failures, err := db.ListRouteApplyFailuresByApp(context.Background(), "my-app")
	if err != nil || len(failures) != 1 || !strings.Contains(failures[0].Detail, context.Canceled.Error()) {
		t.Fatalf("canceled apply lost failure state = %#v, %v", failures, err)
	}
	events, err := db.ListEventsByApp(context.Background(), "my-app")
	if err != nil || len(events) != 2 || events[1].Type != "router.reload_failed" {
		t.Fatalf("canceled apply lost terminal audit = %#v, %v", events, err)
	}
}

type cancelingRouteApplier struct {
	cancel context.CancelFunc
}

func (a cancelingRouteApplier) SyncRoutes(ctx context.Context, _ []Route) error {
	a.cancel()
	return ctx.Err()
}

func TestServiceReconcileRejectsConflictingStoredPublicSocket(t *testing.T) {
	ctx := context.Background()
	db, adapter, service := newInitialRouteService(t, "my-app", "example.com")
	now := time.Now().UTC()
	if err := db.CreateApp(ctx, app.App{ID: "api-app", Name: "api-app", NodeID: "local", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for appID, address := range map[string]string{"my-app": "example.com", "api-app": "http://example.com:443"} {
		if err := db.AttachDomain(ctx, app.Domain{ID: appID, AppID: appID, ServiceName: "web", DomainName: address, Port: 3000, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Reconcile(ctx); err == nil || !strings.Contains(err.Error(), "duplicate routing intent") {
		t.Fatalf("conflicting registry snapshot was applied: %v", err)
	}
	routes, err := adapter.Routes(ctx)
	if err != nil || len(routes) != 0 {
		t.Fatalf("invalid snapshot changed active routes = %#v, %v", routes, err)
	}
}

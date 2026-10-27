package router_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
)

func TestServiceMutationsRebuildAllRoutesAndRetainAudit(t *testing.T) {
	ctx := context.Background()
	db, adapter, cfg := newMutationFixture(t)
	service := router.NewService(cfg)
	for _, request := range []router.AttachRequest{
		{AppID: "my-app", ServiceName: "web", DomainName: "www.example.com", Port: 3000},
		{AppID: "api-app", ServiceName: "api", DomainName: "api.example.com", Port: 4000},
	} {
		if err := service.Attach(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	want := []router.Route{
		{AppID: "api-app", ServiceName: "api", DomainName: "api.example.com", Port: 4000, HTTPS: true},
		{AppID: "my-app", ServiceName: "web", DomainName: "www.example.com", Port: 3000, HTTPS: true},
	}
	if got := adapter.lastSync(); !reflect.DeepEqual(got, want) {
		t.Fatalf("routes = %#v, want %#v", got, want)
	}
	if err := service.Detach(ctx, "my-app", "www.example.com"); err != nil {
		t.Fatal(err)
	}
	if got := adapter.lastSync(); !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("routes after detach = %#v, want api route", got)
	}
	domains, err := db.ListDomainsByApp(ctx, "my-app")
	if err != nil || len(domains) != 0 {
		t.Fatalf("domains after detach = %#v, %v", domains, err)
	}
	events, err := db.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mutationEventTypes(events), []string{"domain.attached", "router.reloaded", "domain.detached", "router.reloaded"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
	if err := service.Detach(ctx, "api-app", "api.example.com"); err != nil {
		t.Fatal(err)
	}
	if got := adapter.lastSync(); len(got) != 0 {
		t.Fatalf("last-domain detach routes = %#v, want empty", got)
	}
}

func TestServiceValidatesPortBeforeMutation(t *testing.T) {
	for _, port := range []int{-1, 0, 1, 65535, 65536} {
		t.Run(fmt.Sprint(port), func(t *testing.T) {
			ctx := context.Background()
			db, adapter, cfg := newMutationFixture(t)
			err := router.NewService(cfg).Attach(ctx, router.AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: port})
			domains, listErr := db.ListDomainsByApp(ctx, "my-app")
			if listErr != nil {
				t.Fatal(listErr)
			}
			if port >= 1 && port <= 65535 {
				if err != nil || len(domains) != 1 || domains[0].Port != port || adapter.syncCount() != 1 {
					t.Fatalf("valid port: error=%v domains=%#v syncs=%d", err, domains, adapter.syncCount())
				}
			} else {
				events, eventErr := db.ListEventsByApp(ctx, "my-app")
				if err == nil || len(domains) != 0 || adapter.syncCount() != 0 || eventErr != nil || len(events) != 0 {
					t.Fatalf("invalid port mutated state: error=%v domains=%#v events=%#v event error=%v syncs=%d", err, domains, events, eventErr, adapter.syncCount())
				}
			}
		})
	}
}

func TestServiceKeepsDistinctDomainIdentitiesAndPublicOwnership(t *testing.T) {
	ctx := context.Background()
	db, adapter, cfg := newMutationFixture(t)
	service := router.NewService(cfg)
	for _, address := range []string{"a-b.example.com", "a.b.example.com"} {
		if err := service.Attach(ctx, router.AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: address, Port: 3000}); err != nil {
			t.Fatal(err)
		}
	}
	domains, err := db.ListDomainsByApp(ctx, "my-app")
	if err != nil || len(domains) != 2 || domains[0].ID == domains[1].ID {
		t.Fatalf("colliding domain identities = %#v, %v", domains, err)
	}
	before := adapter.syncCount()
	if err := service.Attach(ctx, router.AttachRequest{AppID: "api-app", ServiceName: "api", DomainName: "https://A-B.EXAMPLE.COM:443", Port: 4000}); err == nil {
		t.Fatal("second app acquired the same public HTTPS address")
	}
	if got := adapter.syncCount(); got != before {
		t.Fatalf("rejected ownership conflict reloaded routes: %d -> %d", before, got)
	}
	other, err := db.ListDomainsByApp(ctx, "api-app")
	if err != nil || len(other) != 0 {
		t.Fatalf("conflicting app domains = %#v, %v", other, err)
	}
	if err := service.Attach(ctx, router.AttachRequest{AppID: "my-app", ServiceName: "replacement", DomainName: "https://a-b.example.com:443", Port: 3100}); err != nil {
		t.Fatal(err)
	}
	domains, err = db.ListDomainsByApp(ctx, "my-app")
	if err != nil || len(domains) != 2 {
		t.Fatalf("same-owner alias created a duplicate public address: %#v, %v", domains, err)
	}
}

func TestServiceOwnsPublicSocketAcrossProtocolChanges(t *testing.T) {
	for _, test := range []struct {
		first, replacement string
		https              bool
	}{
		{first: "example.com", replacement: "http://example.com:443"},
		{first: "http://example.com", replacement: "https://example.com:80", https: true},
		{first: "http://example.com:8080", replacement: "https://example.com:8080", https: true},
	} {
		t.Run(test.replacement, func(t *testing.T) {
			ctx := context.Background()
			db, adapter, cfg := newMutationFixture(t)
			service := router.NewService(cfg)
			if err := service.Attach(ctx, router.AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: test.first, Port: 3000}); err != nil {
				t.Fatal(err)
			}
			original, err := db.ListDomainsByApp(ctx, "my-app")
			if err != nil || len(original) != 1 {
				t.Fatalf("original route = %#v, %v", original, err)
			}
			if err := service.Attach(ctx, router.AttachRequest{AppID: "api-app", ServiceName: "api", DomainName: test.replacement, Port: 4000}); err == nil {
				t.Fatal("another app claimed the same public host and listen port with a different protocol")
			}
			if adapter.syncCount() != 1 {
				t.Fatalf("conflicting request applied routes %d times", adapter.syncCount())
			}
			if err := service.Attach(ctx, router.AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: test.replacement, Port: 3100}); err != nil {
				t.Fatal(err)
			}
			domains, err := db.ListDomains(ctx)
			if err != nil || len(domains) != 1 || domains[0].ID != original[0].ID || domains[0].DomainName != test.replacement || domains[0].Port != 3100 || domains[0].HTTPS != test.https {
				t.Fatalf("protocol update lost address ownership: %#v, %v", domains, err)
			}
		})
	}
}

func TestServiceRetainsEachApplyFailureAndClearsAfterCompleteRebuild(t *testing.T) {
	ctx := context.Background()
	db, adapter, cfg := newMutationFixture(t)
	service := router.NewService(cfg)
	applyErr := errors.New("caddy reload failed")
	adapter.setError(applyErr)
	request := router.AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000}
	for range 2 {
		if err := service.Attach(ctx, request); !errors.Is(err, applyErr) {
			t.Fatalf("Attach = %v, want %v", err, applyErr)
		}
	}
	failures, err := db.ListRouteApplyFailuresByApp(ctx, "my-app")
	if err != nil || len(failures) != 1 || failures[0].Operation != store.RouteApplyAttach || failures[0].Detail != applyErr.Error() {
		t.Fatalf("attach failure = %#v, %v", failures, err)
	}
	events, err := db.ListEventsByApp(ctx, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mutationEventTypes(events), []string{"domain.attached", "router.reload_failed", "domain.attached", "router.reload_failed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
	if err := service.Detach(ctx, "my-app", "example.com"); !errors.Is(err, applyErr) {
		t.Fatalf("Detach = %v, want %v", err, applyErr)
	}
	domains, err := db.ListDomainsByApp(ctx, "my-app")
	if err != nil || len(domains) != 0 {
		t.Fatalf("failed apply must retain detached intent: %#v, %v", domains, err)
	}
	failures, err = db.ListRouteApplyFailuresByApp(ctx, "my-app")
	if err != nil || len(failures) != 1 || failures[0].Operation != store.RouteApplyDetach {
		t.Fatalf("detach failure = %#v, %v", failures, err)
	}
	adapter.setError(nil)
	if err := service.Attach(ctx, router.AttachRequest{AppID: "api-app", ServiceName: "api", DomainName: "api.example.com", Port: 4000}); err != nil {
		t.Fatal(err)
	}
	failures, err = db.ListRouteApplyFailuresByApp(ctx, "my-app")
	if err != nil || len(failures) != 0 {
		t.Fatalf("complete route rebuild did not clear other-app failure: %#v, %v", failures, err)
	}
}

func TestServiceAggregatesApplyAndFailurePersistenceErrors(t *testing.T) {
	ctx := context.Background()
	db, adapter, cfg := newMutationFixture(t)
	applyErr := errors.New("caddy unavailable")
	failureErr := errors.New("failure state unavailable")
	eventErr := errors.New("failure event unavailable")
	adapter.setError(applyErr)
	cfg.Store = &mutationFaultStore{SQLiteStore: db, failureErr: failureErr, eventErr: eventErr}
	err := router.NewService(cfg).Attach(ctx, router.AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000})
	for _, want := range []error{applyErr, failureErr, eventErr} {
		if !errors.Is(err, want) {
			t.Fatalf("Attach = %v, want %v", err, want)
		}
	}
	domains, listErr := db.ListDomainsByApp(ctx, "my-app")
	if listErr != nil || len(domains) != 1 {
		t.Fatalf("domain intent was lost: %#v, %v", domains, listErr)
	}
}

func TestServiceCleanupFailureDoesNotBecomeApplyFailure(t *testing.T) {
	ctx := context.Background()
	db, _, cfg := newMutationFixture(t)
	cleanupErr := errors.New("database is read-only")
	cfg.Store = &mutationFaultStore{SQLiteStore: db, clearErr: cleanupErr}
	err := router.NewService(cfg).Attach(ctx, router.AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000})
	if !errors.Is(err, cleanupErr) || !strings.Contains(err.Error(), "reloaded") {
		t.Fatalf("Attach = %v, want applied routes with cleanup failure", err)
	}
	events, listErr := db.ListEventsByApp(ctx, "my-app")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if got, want := mutationEventTypes(events), []string{"domain.attached", "router.reloaded"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
	failures, listErr := db.ListRouteApplyFailuresByApp(ctx, "my-app")
	if listErr != nil || len(failures) != 0 {
		t.Fatalf("successful apply was recorded as failed: %#v, %v", failures, listErr)
	}
}

func TestServiceRequiresOperationalAdapterAndLockBeforeMutation(t *testing.T) {
	for _, missing := range []string{"router", "locks"} {
		t.Run(missing, func(t *testing.T) {
			ctx := context.Background()
			db, adapter, cfg := newMutationFixture(t)
			if missing == "router" {
				cfg.Router = nil
			} else {
				cfg.LocksDir = ""
			}
			service := router.NewService(cfg)
			if err := service.Attach(ctx, router.AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000}); err == nil {
				t.Fatal("Attach succeeded without operational configuration")
			}
			domains, err := db.ListDomains(ctx)
			if err != nil || len(domains) != 0 || adapter.syncCount() != 0 {
				t.Fatalf("missing config mutated routing: %#v, %v, syncs=%d", domains, err, adapter.syncCount())
			}
		})
	}
}

func TestServiceRejectsAttachmentWhenAppIsRemovedAfterLookup(t *testing.T) {
	ctx := context.Background()
	db, adapter, cfg := newMutationFixture(t)
	cfg.Store = &removeBeforeAttachmentStore{SQLiteStore: db}
	err := router.NewService(cfg).Attach(ctx, router.AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Attach = %v, want removed-app error", err)
	}
	domains, listErr := db.ListDomains(ctx)
	if listErr != nil || len(domains) != 0 || adapter.syncCount() != 0 {
		t.Fatalf("removed app acquired an orphan route: %#v, %v, syncs=%d", domains, listErr, adapter.syncCount())
	}
}

func TestServicesShareLockAcrossPersistenceAndApply(t *testing.T) {
	ctx := context.Background()
	db, adapter, cfg := newMutationFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	adapter.entered, adapter.release = entered, release
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	first, second := router.NewService(cfg), router.NewService(cfg)
	done := make(chan error, 1)
	go func() {
		done <- first.Attach(ctx, router.AttachRequest{AppID: "my-app", ServiceName: "web", DomainName: "example.com", Port: 3000})
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("first apply did not block: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("first apply never started")
	}
	blocked, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	err := second.Attach(blocked, router.AttachRequest{AppID: "api-app", ServiceName: "api", DomainName: "api.example.com", Port: 4000})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("concurrent owner = %v, want route lock deadline", err)
	}
	domains, listErr := db.ListDomainsByApp(ctx, "api-app")
	if listErr != nil || len(domains) != 0 || adapter.syncCount() != 1 {
		t.Fatalf("blocked mutation escaped lock: domains=%#v error=%v syncs=%d", domains, listErr, adapter.syncCount())
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := second.Attach(ctx, router.AttachRequest{AppID: "api-app", ServiceName: "api", DomainName: "api.example.com", Port: 4000}); err != nil {
		t.Fatal(err)
	}
	if got := adapter.lastSync(); len(got) != 2 {
		t.Fatalf("final complete route set = %#v, want both apps", got)
	}
}

func newMutationFixture(t *testing.T) (*store.SQLiteStore, *mutationAdapter, router.ServiceConfig) {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().UTC()
	for _, name := range []string{"my-app", "api-app"} {
		if err := db.CreateApp(ctx, app.App{ID: name, Name: name, NodeID: "local", Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &mutationAdapter{}
	return db, adapter, router.ServiceConfig{Store: db, Router: adapter, LocksDir: t.TempDir()}
}

func mutationEventTypes(events []app.Event) []string {
	result := make([]string, 0, len(events))
	for _, event := range events {
		result = append(result, event.Type)
	}
	return result
}

type mutationAdapter struct {
	mu      sync.Mutex
	syncs   [][]router.Route
	err     error
	entered chan struct{}
	release <-chan struct{}
}

func (a *mutationAdapter) SyncRoutes(ctx context.Context, routes []router.Route) error {
	copied := append([]router.Route{}, routes...)
	sort.Slice(copied, func(i, j int) bool { return copied[i].DomainName < copied[j].DomainName })
	a.mu.Lock()
	first := len(a.syncs) == 0
	a.syncs = append(a.syncs, copied)
	err := a.err
	a.mu.Unlock()
	if first && a.entered != nil {
		close(a.entered)
		select {
		case <-a.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
func (a *mutationAdapter) setError(err error) { a.mu.Lock(); defer a.mu.Unlock(); a.err = err }
func (a *mutationAdapter) syncCount() int     { a.mu.Lock(); defer a.mu.Unlock(); return len(a.syncs) }
func (a *mutationAdapter) lastSync() []router.Route {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]router.Route{}, a.syncs[len(a.syncs)-1]...)
}

type mutationFaultStore struct {
	*store.SQLiteStore
	failureErr error
	eventErr   error
	clearErr   error
}

type removeBeforeAttachmentStore struct{ *store.SQLiteStore }

func (s *removeBeforeAttachmentStore) AttachDomain(ctx context.Context, model app.Domain) error {
	if err := s.SQLiteStore.DeleteApp(ctx, model.AppID); err != nil {
		return err
	}
	return s.SQLiteStore.AttachDomain(ctx, model)
}

func (s *mutationFaultStore) UpsertRouteApplyFailure(ctx context.Context, failure store.RouteApplyFailure) error {
	if s.failureErr != nil {
		return s.failureErr
	}
	return s.SQLiteStore.UpsertRouteApplyFailure(ctx, failure)
}
func (s *mutationFaultStore) CreateEvent(ctx context.Context, event app.Event) error {
	if event.Type == "router.reload_failed" && s.eventErr != nil {
		return s.eventErr
	}
	return s.SQLiteStore.CreateEvent(ctx, event)
}
func (s *mutationFaultStore) ClearRouteApplyFailures(ctx context.Context) error {
	if s.clearErr != nil {
		return s.clearErr
	}
	return s.SQLiteStore.ClearRouteApplyFailures(ctx)
}

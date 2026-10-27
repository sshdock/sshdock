package deployment

import (
	"context"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
	"testing"
	"time"
)

type routeSyncer interface {
	SyncRoutes(context.Context, []router.Route) error
}

func testRouting(t *testing.T, db store.Store, applier routeSyncer, now func() time.Time) *router.Service {
	t.Helper()
	return router.NewService(router.ServiceConfig{Store: db, Router: applier, LocksDir: t.TempDir(), Now: now})
}

func newTestExecutor(t *testing.T, cfg Config) *Executor {
	t.Helper()
	if cfg.Routing == nil {
		cfg.Routing = testRouting(t, cfg.Store.(store.Store), router.NewFakeRouter(), cfg.Now)
	}
	return NewExecutor(cfg)
}

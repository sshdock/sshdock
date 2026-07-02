package router

import (
	"context"
	"errors"
	"testing"
)

func TestFakeRouterAttachDetachReloadAndList(t *testing.T) {
	ctx := context.Background()
	router := NewFakeRouter()
	route := Route{
		AppID:       "app_1",
		ServiceName: "web",
		DomainName:  "example.com",
		Port:        3000,
		HTTPS:       true,
	}

	if err := router.AttachDomain(ctx, route); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}

	routes, err := router.Routes(ctx)
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}
	if len(routes) != 1 || routes[0] != route {
		t.Fatalf("Routes = %#v, want [%#v]", routes, route)
	}

	if err := router.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if router.ReloadCount != 1 {
		t.Fatalf("ReloadCount = %d, want 1", router.ReloadCount)
	}

	if err := router.DetachDomain(ctx, route.DomainName); err != nil {
		t.Fatalf("DetachDomain: %v", err)
	}
	routes, err = router.Routes(ctx)
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}
	if len(routes) != 0 {
		t.Fatalf("Routes after detach = %#v, want empty", routes)
	}

	replacementRoutes := []Route{
		{AppID: "app_1", ServiceName: "web", DomainName: "two.example.com", Port: 3000, HTTPS: true},
		{AppID: "app_2", ServiceName: "api", DomainName: "one.example.com", Port: 4000, HTTPS: true},
	}
	if err := router.SyncRoutes(ctx, replacementRoutes); err != nil {
		t.Fatalf("SyncRoutes: %v", err)
	}
	routes, err = router.Routes(ctx)
	if err != nil {
		t.Fatalf("Routes after sync: %v", err)
	}
	wantRoutes := []Route{replacementRoutes[1], replacementRoutes[0]}
	if len(routes) != len(wantRoutes) {
		t.Fatalf("Routes after sync len = %d, want %d: %#v", len(routes), len(wantRoutes), routes)
	}
	for i := range wantRoutes {
		if routes[i] != wantRoutes[i] {
			t.Fatalf("Routes after sync[%d] = %#v, want %#v", i, routes[i], wantRoutes[i])
		}
	}
}

func TestFakeRouterFailureModes(t *testing.T) {
	ctx := context.Background()
	attachErr := errors.New("attach failed")
	detachErr := errors.New("detach failed")
	reloadErr := errors.New("reload failed")
	routesErr := errors.New("routes failed")
	router := NewFakeRouter()
	router.AttachErr = attachErr
	router.DetachErr = detachErr
	router.ReloadErr = reloadErr
	router.RoutesErr = routesErr

	if err := router.AttachDomain(ctx, Route{}); !errors.Is(err, attachErr) {
		t.Fatalf("AttachDomain error = %v, want %v", err, attachErr)
	}
	if err := router.DetachDomain(ctx, "example.com"); !errors.Is(err, detachErr) {
		t.Fatalf("DetachDomain error = %v, want %v", err, detachErr)
	}
	if err := router.Reload(ctx); !errors.Is(err, reloadErr) {
		t.Fatalf("Reload error = %v, want %v", err, reloadErr)
	}
	if err := router.SyncRoutes(ctx, []Route{{DomainName: "example.com"}}); !errors.Is(err, attachErr) {
		t.Fatalf("SyncRoutes error = %v, want %v", err, attachErr)
	}
	if _, err := router.Routes(ctx); !errors.Is(err, routesErr) {
		t.Fatalf("Routes error = %v, want %v", err, routesErr)
	}
}

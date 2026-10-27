package router

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFakeRouterSyncReplacesRoutesAndListsSortedSnapshot(t *testing.T) {
	ctx := context.Background()
	router := NewFakeRouter()
	if err := router.SyncRoutes(ctx, []Route{{DomainName: "old.example.com", Port: 3000}}); err != nil {
		t.Fatal(err)
	}
	replacement := []Route{
		{AppID: "app_1", ServiceName: "web", DomainName: "two.example.com", Port: 3000, HTTPS: true},
		{AppID: "app_2", ServiceName: "api", DomainName: "one.example.com", Port: 4000, HTTPS: true},
	}
	if err := router.SyncRoutes(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	routes, err := router.Routes(ctx)
	want := []Route{replacement[1], replacement[0]}
	if err != nil || !reflect.DeepEqual(routes, want) {
		t.Fatalf("Routes = %#v, %v; want %#v", routes, err, want)
	}
	if err := router.SyncRoutes(ctx, nil); err != nil {
		t.Fatal(err)
	}
	routes, err = router.Routes(ctx)
	if err != nil || len(routes) != 0 {
		t.Fatalf("empty snapshot left routes = %#v, %v", routes, err)
	}
}

func TestFakeRouterFailureModesPreserveAppliedSnapshot(t *testing.T) {
	ctx := context.Background()
	router := NewFakeRouter()
	existing := []Route{{DomainName: "example.com", Port: 3000}}
	if err := router.SyncRoutes(ctx, existing); err != nil {
		t.Fatal(err)
	}
	syncErr := errors.New("sync failed")
	router.SyncErr = syncErr
	if err := router.SyncRoutes(ctx, []Route{{DomainName: "replacement.example.com"}}); !errors.Is(err, syncErr) {
		t.Fatalf("SyncRoutes error = %v, want %v", err, syncErr)
	}
	routes, err := router.Routes(ctx)
	if err != nil || !reflect.DeepEqual(routes, existing) {
		t.Fatalf("failed sync changed applied routes = %#v, %v", routes, err)
	}
	routesErr := errors.New("routes failed")
	router.RoutesErr = routesErr
	if _, err := router.Routes(ctx); !errors.Is(err, routesErr) {
		t.Fatalf("Routes error = %v, want %v", err, routesErr)
	}
}

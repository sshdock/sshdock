package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/store"
)

func TestAppCreationRejectsInvalidNamesWithoutSideEffects(t *testing.T) {
	for _, test := range []struct{ name, suggestion string }{
		{"", "app"},
		{"My_App", "my-app"},
		{"App", "app"},
		{"my_app", "my-app"},
		{"my.app", "my-app"},
		{"my app", "my-app"},
		{"-app", "app"},
		{"app-", "app"},
		{strings.Repeat("a", 64), strings.Repeat("a", 63)},
		{"應用", "app"},
		{"../app", "app"},
		{`one\app`, "one-app"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db := newStoreBackendTestStore(t, ctx)
			if err := db.SetServerConfig(ctx, store.ServerConfig{GitHost: "configured.example.com", UpdatedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			spy := &appCreationStore{Store: db}
			root := t.TempDir()
			setupper := &fakeReceiveRepoSetupper{}
			persistent := NewStoreBackend(spy, StoreBackendConfig{AppsDir: filepath.Join(root, "apps"), GitHost: "fallback.example.com", RepoSetupper: setupper})
			memory := NewMemoryBackend("configured.example.com")
			for _, backend := range []interface {
				CreateApp(string) (App, string, error)
			}{memory, persistent} {
				model, remote, err := backend.CreateApp(test.name)
				want := fmt.Sprintf("app name %q is not a normalized DNS label; use %q\nrun: git remote set-url sshdock git@configured.example.com:%s.git", test.name, test.suggestion, test.suggestion)
				var invalid *domaincfg.InvalidAppNameError
				if model != (App{}) || remote != "" || err == nil || err.Error() != want || !errors.As(err, &invalid) {
					t.Fatalf("CreateApp(%q) = %#v, %q, %v; want %q", test.name, model, remote, err, want)
				}
			}
			if spy.getCalls != 0 || spy.createCalls != 0 || spy.listCalls != 0 || len(setupper.apps) != 0 || len(memory.apps) != 0 {
				t.Fatalf("invalid name reached registry/repo: gets=%d creates=%d lists=%d setup=%#v memory=%#v", spy.getCalls, spy.createCalls, spy.listCalls, setupper.apps, memory.apps)
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatalf("invalid name created files: %v, %v", entries, err)
			}
			if models, err := db.ListApps(ctx); err != nil || len(models) != 0 {
				t.Fatalf("invalid name changed registry: %#v, %v", models, err)
			}
		})
	}
}

func TestAppCreationUsesExactValidNamesWithoutRegistryScan(t *testing.T) {
	ctx := context.Background()
	db := newStoreBackendTestStore(t, ctx)
	if err := db.CreateApp(ctx, appmodel.App{ID: "foo.bar", Name: "foo.bar"}); err != nil {
		t.Fatal(err)
	}
	spy := &appCreationStore{Store: db}
	setupper := &fakeReceiveRepoSetupper{}
	persistent := NewStoreBackend(spy, StoreBackendConfig{AppsDir: filepath.Join(t.TempDir(), "apps"), GitHost: "server", RepoSetupper: setupper})
	memory := NewMemoryBackend("server")
	memory.apps["foo.bar"] = App{Name: "foo.bar"}
	names := []string{"a", strings.Repeat("a", 63), "foo-bar", "foo--bar"}
	for _, backend := range []interface {
		CreateApp(string) (App, string, error)
	}{memory, persistent} {
		for _, name := range names {
			model, _, err := backend.CreateApp(name)
			if err != nil || model.Name != name {
				t.Fatalf("CreateApp(%q) = %#v, %v", name, model, err)
			}
		}
		if _, _, err := backend.CreateApp("foo-bar"); err == nil || err.Error() != `app "foo-bar" already exists` {
			t.Fatalf("ordinary duplicate check = %v", err)
		}
	}
	if spy.listCalls != 0 || spy.createCalls != len(names) || len(setupper.apps) != len(names) {
		t.Fatalf("creation scan/writes/setup = %d/%d/%#v", spy.listCalls, spy.createCalls, setupper.apps)
	}
	if _, err := db.GetApp(ctx, "foo.bar"); err != nil {
		t.Fatalf("existing registry data was changed: %v", err)
	}
}

type appCreationStore struct {
	store.Store
	getCalls    int
	createCalls int
	listCalls   int
}

func (s *appCreationStore) GetApp(ctx context.Context, id string) (appmodel.App, error) {
	s.getCalls++
	return s.Store.GetApp(ctx, id)
}

func (s *appCreationStore) CreateApp(ctx context.Context, model appmodel.App) error {
	s.createCalls++
	return s.Store.CreateApp(ctx, model)
}

func (s *appCreationStore) ListApps(context.Context) ([]appmodel.App, error) {
	s.listCalls++
	return nil, errors.New("creation must not scan the app registry")
}

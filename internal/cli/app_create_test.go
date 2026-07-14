package cli

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sshdock/sshdock/internal/store"
)

func TestStoreBackendRejectsInvalidAppNameWithRemoteUpdateCommand(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	setupper := &fakeReceiveRepoSetupper{}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		AppsDir:      filepath.Join(t.TempDir(), "apps"),
		GitHost:      "sshdock.example.com",
		RepoSetupper: setupper,
	})

	// When
	_, _, err := backend.CreateApp("My_App")

	// Then
	want := "app name \"My_App\" is not a normalized DNS label; use \"my-app\"\nrun: git remote set-url sshdock git@sshdock.example.com:my-app.git"
	if err == nil || err.Error() != want {
		t.Fatalf("CreateApp error = %q, want %q", err, want)
	}
	if len(setupper.apps) != 0 {
		t.Fatalf("receive repo setup apps = %#v, want none", setupper.apps)
	}
	if _, getErr := sqlite.GetApp(ctx, "My_App"); !errors.Is(getErr, store.ErrNotFound) {
		t.Fatalf("GetApp error = %v, want ErrNotFound", getErr)
	}
}

func TestMemoryBackendMatchesInvalidAppNameBoundary(t *testing.T) {
	// Given
	backend := NewMemoryBackend("sshdock.example.com")

	// When
	_, _, err := backend.CreateApp("My_App")

	// Then
	want := "app name \"My_App\" is not a normalized DNS label; use \"my-app\"\nrun: git remote set-url sshdock git@sshdock.example.com:my-app.git"
	if err == nil || err.Error() != want {
		t.Fatalf("CreateApp error = %q, want %q", err, want)
	}
}

func TestMemoryBackendRejectsRuntimeIdentityCollisionWithLegacyApp(t *testing.T) {
	// Given
	backend := NewMemoryBackend("sshdock.example.com")
	backend.apps["foo.bar"] = App{Name: "foo.bar", Status: "created", NodeID: "local"}

	// When
	_, _, err := backend.CreateApp("foo-bar")

	// Then
	want := `app name "foo-bar" conflicts with existing app "foo.bar" because both use runtime identity "sshdock_foo-bar"; choose another app name`
	if err == nil || err.Error() != want {
		t.Fatalf("CreateApp error = %q, want %q", err, want)
	}
	if _, found := backend.apps["foo-bar"]; found {
		t.Fatal("foo-bar was created despite runtime identity collision")
	}
}

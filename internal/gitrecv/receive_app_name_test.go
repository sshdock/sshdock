package gitrecv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
)

func TestReceivePackServiceRejectsInvalidNameBeforeLockAndStore(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, test := range []struct{ name, suggestion string }{
			{"My_App", "my-app"},
			{"App", "app"},
			{"my_app", "my-app"},
			{"my.app", "my-app"},
			{"My App", "my-app"},
			{"-app", "app"},
			{"app-", "app"},
			{strings.Repeat("a", 64), strings.Repeat("a", 63)},
			{"應用", "app"},
		} {
			t.Run(fmt.Sprintf("existing=%t/%s", existing, test.name), func(t *testing.T) {
				ctx := context.Background()
				db := newReceiveTestStore(t, ctx)
				root := t.TempDir()
				appsDir, locksDir := filepath.Join(root, "apps"), filepath.Join(root, "locks")
				model := app.App{ID: test.name, Name: test.name, NodeID: "local", RepoPath: filepath.Join(appsDir, test.name, "repo.git"), Status: app.AppStatusCreated, CreatedAt: time.Now().UTC()}
				if existing {
					if err := db.CreateApp(ctx, model); err != nil {
						t.Fatal(err)
					}
				}
				spy := &receiveNameStore{receivePackStore: db}
				executor, receiver := &recordingGitExecutor{}, &recordingReceivePackRunner{}
				service := NewReceivePackService(ReceivePackServiceConfig{
					Store: spy, AppsDir: appsDir, LocksDir: locksDir,
					RepoManager: NewRepoManager(RepoManagerConfig{AppsDir: appsDir, GitHost: "configured.example.com", Executor: executor}), ReceivePackRunner: receiver,
				})
				err := service.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack '" + test.name + ".git'"})
				want := fmt.Sprintf("app name %q is not a normalized DNS label; use %q\nrun: git remote set-url sshdock git@configured.example.com:%s.git", test.name, test.suggestion, test.suggestion)
				var invalid *domaincfg.InvalidAppNameError
				if err == nil || err.Error() != want || !errors.As(err, &invalid) {
					t.Fatalf("Receive error = %v, want %q", err, want)
				}
				if spy.getCalls != 0 || len(executor.Commands) != 0 || receiver.repoPath != "" {
					t.Fatalf("invalid name reached store/Git/receiver: gets=%d commands=%#v receiver=%#v", spy.getCalls, executor.Commands, receiver)
				}
				if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
					t.Fatalf("invalid name created lock or app files: %v, %v", entries, err)
				}
				models, err := db.ListApps(ctx)
				wantCount := 0
				if existing {
					wantCount = 1
				}
				if err != nil || len(models) != wantCount || (existing && models[0] != model) {
					t.Fatalf("invalid name changed registry: %#v, %v", models, err)
				}
			})
		}
	}
}

func TestReceivePackServiceCanonicalNameDoesNotScanLegacyRegistryNames(t *testing.T) {
	ctx := context.Background()
	db := newReceiveTestStore(t, ctx)
	if err := db.CreateApp(ctx, app.App{ID: "foo.bar", Name: "foo.bar", Status: app.AppStatusCreated}); err != nil {
		t.Fatal(err)
	}
	appsDir := filepath.Join(t.TempDir(), "apps")
	receiver := &recordingReceivePackRunner{}
	service := NewReceivePackService(ReceivePackServiceConfig{
		Store: db, AppsDir: appsDir,
		RepoManager: NewRepoManager(RepoManagerConfig{AppsDir: appsDir, Executor: &recordingGitExecutor{}}), ReceivePackRunner: receiver,
	})
	if err := service.Receive(ctx, ReceivePackRequest{OriginalCommand: "git-receive-pack 'foo-bar.git'"}); err != nil {
		t.Fatal(err)
	}
	if receiver.repoPath != filepath.Join(appsDir, "foo-bar", "repo.git") {
		t.Fatalf("canonical repository = %q", receiver.repoPath)
	}
	if _, err := db.GetApp(ctx, "foo.bar"); err != nil {
		t.Fatalf("existing registry record was changed: %v", err)
	}
}

type receiveNameStore struct {
	receivePackStore
	getCalls int
}

func (s *receiveNameStore) GetApp(ctx context.Context, id string) (app.App, error) {
	s.getCalls++
	return s.receivePackStore.GetApp(ctx, id)
}

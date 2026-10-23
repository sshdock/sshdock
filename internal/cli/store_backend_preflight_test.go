package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

func TestStoreBackendPreflightUsesCurrentWorktreeConfigWithoutMutations(t *testing.T) {
	for _, withConfig := range []bool{false, true} {
		t.Run(fmt.Sprintf("stored config=%t", withConfig), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			databasePath := filepath.Join(root, "sshdock.db")
			seed, err := store.OpenSQLite(ctx, databasePath)
			if err != nil {
				t.Fatal(err)
			}
			appsDir := filepath.Join(root, "apps")
			seedRecoveryApp(t, ctx, seed, appsDir, time.Now().UTC())
			model, err := seed.GetApp(ctx, "my-app")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(model.RepoPath, "refs", "heads"), 0o755); err != nil {
				t.Fatal(err)
			}
			checkedOut := strings.Repeat("a", 40)
			for path, text := range map[string]string{
				filepath.Join(model.RepoPath, "HEAD"):                  checkedOut + "\n",
				filepath.Join(model.RepoPath, "refs", "heads", "main"): strings.Repeat("b", 40) + "\n",
			} {
				if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			keyPath := filepath.Join(root, "keys", "config.key")
			secret := "database-password"
			if withConfig {
				service := appconfig.NewService(seed, keyPath)
				if err := service.Set(ctx, appconfig.SetRequest{AppID: model.ID, Name: "PASSWORD", Value: []byte(secret)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := seed.Close(); err != nil {
				t.Fatal(err)
			}
			before := preflightFileSnapshot(t, root)
			readOnly, err := store.OpenSQLiteReadOnly(ctx, databasePath)
			if err != nil {
				t.Fatal(err)
			}
			checker := &recordingPreflightRunner{FakeRunner: &compose.FakeRunner{}, report: compose.PreflightReport{
				OK: true, Scope: "server", ComposePath: filepath.Join(model.WorktreePath, "compose.yml"),
			}}
			if withConfig {
				checker.report.Checks = []compose.PreflightCheck{{Name: "config", Status: "warn", Detail: "value=" + secret, Fix: "replace " + secret}}
			}
			backend := NewStoreBackend(readOnly, StoreBackendConfig{RecoveryRunner: checker, ConfigManager: appconfig.NewService(readOnly, keyPath)})
			report, err := backend.Preflight(model.ID)
			if err != nil || !report.OK || len(checker.requests) != 1 {
				t.Fatalf("Preflight = %#v, %v; requests=%v", report, err, checker.requests)
			}
			request := checker.requests[0]
			if request.AppName != model.ID || request.ProjectDir != model.WorktreePath || request.Scope != "server" || request.Env[compose.GitSHAEnv] != checkedOut {
				t.Fatalf("preflight selected wrong runtime identity: %#v", request)
			}
			if withConfig {
				if request.Env["PASSWORD"] != secret || request.RedactionValues[model.ID+"/PASSWORD"] != secret || strings.Contains(report.Checks[0].Detail+report.Checks[0].Fix, secret) {
					t.Fatalf("preflight config resolution or redaction incorrect: %#v", report)
				}
				checker.err = errors.New("inspection failed for " + secret)
				if _, err := backend.Preflight(model.ID); err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "<redacted>") {
					t.Fatalf("preflight error redaction = %v", err)
				}
			}
			if err := readOnly.Close(); err != nil {
				t.Fatal(err)
			}
			if after := preflightFileSnapshot(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("preflight changed database, keys, Git metadata, or worktree files")
			}
		})
	}
}

func TestStoreBackendPreflightMissingInputsDoNotInitializeState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	seed, err := store.OpenSQLite(ctx, filepath.Join(root, "sshdock.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Close()
	model := app.App{ID: "missing-worktree", Name: "missing-worktree", WorktreePath: filepath.Join(root, "not-created"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := seed.CreateApp(ctx, model); err != nil {
		t.Fatal(err)
	}
	checker := &recordingPreflightRunner{FakeRunner: &compose.FakeRunner{}}
	backend := NewStoreBackend(seed, StoreBackendConfig{RecoveryRunner: checker})
	before := preflightFileSnapshot(t, root)
	report, err := backend.Preflight(model.ID)
	if err != nil || report.OK || len(report.Checks) != 1 || !report.Checks[0].Blocking || len(checker.requests) != 0 {
		t.Fatalf("missing worktree preflight = %#v, %v; requests=%v", report, err, checker.requests)
	}
	if _, err := backend.Preflight("missing-app"); !errors.Is(err, ErrNotFound) || err.Error() != `app "missing-app" not found` {
		t.Fatalf("missing app error = %v", err)
	}
	if after := preflightFileSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("missing-input preflight initialized state")
	}
}

func TestStoreBackendPreflightObservesCaddyWithoutReloading(t *testing.T) {
	for _, test := range []struct {
		name, status string
		err          error
	}{
		{name: "readable", status: "ok"},
		{name: "unavailable", status: "warn", err: errors.New("admin API unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db := newStoreBackendTestStore(t, ctx)
			appsDir := t.TempDir()
			seedRecoveryApp(t, ctx, db, appsDir, time.Now().UTC())
			publisher := &fakeRoutePublisher{RoutesErr: test.err}
			checker := &recordingPreflightRunner{FakeRunner: &compose.FakeRunner{}, report: compose.PreflightReport{OK: true, Scope: "server"}}
			backend := NewStoreBackend(db, StoreBackendConfig{
				RecoveryRunner: checker, Router: publisher,
				ConfigManager: appconfig.NewService(db, filepath.Join(t.TempDir(), "not-created.key")),
			})
			report, err := backend.Preflight("my-app")
			if err != nil || !report.OK || len(publisher.Syncs) != 0 {
				t.Fatalf("Caddy preflight = %#v, %v; syncs=%v", report, err, publisher.Syncs)
			}
			check := report.Checks[len(report.Checks)-1]
			if check.Name != "Caddy" || check.Status != test.status || check.Blocking || !strings.Contains(check.Detail, "reload and public reachability are not verified") || !strings.Contains(check.Fix, "diagnostics") {
				t.Fatalf("Caddy evidence = %#v", check)
			}
		})
	}
}

type recordingPreflightRunner struct {
	*compose.FakeRunner
	requests []compose.PreflightRequest
	report   compose.PreflightReport
	err      error
}

func (r *recordingPreflightRunner) Preflight(_ context.Context, request compose.PreflightRequest) (compose.PreflightReport, error) {
	r.requests = append(r.requests, request)
	return r.report, r.err
}

func preflightFileSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var content []byte
		if info.Mode().IsRegular() {
			content, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		files[path] = fmt.Sprintf("%v:%d:%s", info.Mode(), info.ModTime().UnixNano(), content)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

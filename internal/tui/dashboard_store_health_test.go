package tui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/cli"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

func TestDashboardWithStoreHealthKeepsRedactedHistoryWhenCheckoutHEADIsInvalid(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo, worktree := t.TempDir(), t.TempDir()
	headPath := filepath.Join(repo, "HEAD")
	if err := os.WriteFile(headPath, []byte("invalid-head"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	model := app.App{ID: "my-app", Name: "my-app", NodeID: "local", RepoPath: repo, WorktreePath: worktree, Status: app.AppStatusFailed, CreatedAt: now, UpdatedAt: now}
	if err := db.CreateApp(ctx, model); err != nil {
		t.Fatal(err)
	}
	config := appconfig.NewService(db, filepath.Join(t.TempDir(), "config.key"))
	secret, sha := "encrypted-config-secret", strings.Repeat("a", 40)
	if err := config.Set(ctx, appconfig.SetRequest{AppID: model.ID, Name: "TOKEN", Value: []byte(secret)}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDeployment(ctx, app.Deployment{
		ID: "dep_failed", AppID: model.ID, CommitSHA: sha, Trigger: app.DeploymentTriggerPush,
		Status: app.DeploymentStatusFailed, StartedAt: now, FinishedAt: now,
		FailureStage: "wait services", FailureDetail: sha + " " + secret, ErrorMessage: sha + " " + secret,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateEvent(ctx, app.Event{ID: "evt_failed", AppID: model.ID, Type: "deploy.failed", Message: sha + " " + secret, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	statusFailure := errors.New("runtime status unavailable")
	runner := &compose.FakeRunner{StatusErr: statusFailure}
	backend := cli.NewStoreBackend(db, cli.StoreBackendConfig{RecoveryRunner: runner, ConfigManager: config})
	handler := NewDashboardHandlerWithConfig(db, runner, config, backend)
	snapshot, err := handler.Snapshot(ctx)
	if err != nil {
		t.Fatalf("invalid HEAD hid safely redacted history: %v", err)
	}
	selected, found := snapshot.AppsByID[model.ID]
	if !found || len(selected.Detail.LatestDeployments(5)) != 1 || len(selected.Detail.Events) != 2 {
		t.Fatalf("snapshot lost app history: %#v", snapshot)
	}
	warned := false
	for _, check := range selected.Detail.Health.Checks {
		if check.Name == "services" && check.Status == "warn" && strings.Contains(check.Detail, "repository HEAD") && strings.Contains(check.Detail, "redeploy current remote main") {
			warned = true
		}
	}
	if !warned || len(runner.StatusRequests) != 0 {
		t.Fatalf("services warning missing or runtime status ran: checks=%#v, requests=%#v", selected.Detail.Health.Checks, runner.StatusRequests)
	}
	var rendered bytes.Buffer
	if err := RenderDashboardSnapshot(&rendered, snapshot); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"App my-app", "dep_failed", "deploy.failed", sha, "<redacted>", "redeploy current remote main"} {
		if !strings.Contains(rendered.String(), want) {
			t.Fatalf("rendered history missing %q:\n%s", want, rendered.String())
		}
	}
	if strings.Contains(rendered.String(), secret) {
		t.Fatalf("rendered history leaked encrypted config value:\n%s", rendered.String())
	}
	if err := os.WriteFile(headPath, []byte(sha), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.Snapshot(ctx); !errors.Is(err, statusFailure) || len(runner.StatusRequests) != 1 {
		t.Fatalf("actual runtime status failure must remain fatal: %v, requests=%#v", err, runner.StatusRequests)
	}
}

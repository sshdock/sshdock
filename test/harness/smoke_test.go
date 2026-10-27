package harness

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/deployment"
	"github.com/sshdock/sshdock/internal/router"
	"github.com/sshdock/sshdock/internal/store"
	"github.com/sshdock/sshdock/internal/tui"
)

func TestSmokeVersionCommands(t *testing.T) {
	root := filepath.Join("..", "..")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "sshdock version",
			args: []string{"run", "./cmd/sshdock", "version"},
			want: "sshdock dev\n",
		},
		{
			name: "sshdockd version",
			args: []string{"run", "./cmd/sshdockd", "version"},
			want: "sshdockd dev\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("go", tt.args...)
			cmd.Dir = root

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", tt.name, err, output)
			}

			if string(output) != tt.want {
				t.Fatalf("output = %q, want %q", output, tt.want)
			}
		})
	}
}

func TestSmokeFakeAppLifecycle(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	sqlite, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlite.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	projectDir := filepath.Join("..", "fixtures", "compose", "valid")
	model := app.App{
		ID: "my-app", Name: "my-app", NodeID: "local", WorktreePath: projectDir,
		Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now,
	}
	if err := sqlite.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	fakeRouter := router.NewFakeRouter()
	routing := router.NewService(router.ServiceConfig{Store: sqlite, Router: fakeRouter, LocksDir: t.TempDir(), Now: func() time.Time { return now }})
	runner := &compose.FakeRunner{}
	executor := deployment.NewExecutor(deployment.Config{
		Store: sqlite, Runner: runner, Routing: routing, Now: func() time.Time { return now },
		Checkout: smokeWorktreeCheckout{},
	})
	runAttempt := func(id string) error {
		attempt := app.Deployment{ID: id, AppID: model.ID, ReleaseID: app.ReleaseID(model.ID, "abc123"), CommitSHA: "abc123", Trigger: app.DeploymentTriggerRedeploy, Status: app.DeploymentStatusPending, StartedAt: now}
		if err := sqlite.QueueRedeployment(ctx, attempt); err != nil {
			return err
		}
		claimed, found, err := sqlite.ClaimNextPendingDeployment(ctx)
		if err != nil || !found {
			t.Fatalf("claim queued deployment: found=%t, error=%v", found, err)
		}
		return executor.Execute(ctx, claimed)
	}
	if err := runAttempt("dep_1"); err != nil {
		t.Fatalf("RedeployCurrentMain: %v", err)
	}
	if len(runner.DeployRequests) != 1 || runner.DeployRequests[0].CommitSHA != "abc123" {
		t.Fatalf("deploy requests = %#v", runner.DeployRequests)
	}

	if err := routing.Attach(ctx, router.AttachRequest{AppID: model.ID, ServiceName: "web", DomainName: "example.com", Port: 3000}); err != nil {
		t.Fatal(err)
	}

	apps, err := sqlite.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	attempts, err := sqlite.ListDeploymentsByApp(ctx, model.ID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != app.DeploymentStatusSucceeded {
		t.Fatalf("successful attempts = %#v, error = %v", attempts, err)
	}
	domains, err := sqlite.ListDomainsByApp(ctx, model.ID)
	if err != nil {
		t.Fatal(err)
	}
	view := tui.NewAppListView(apps, map[string]app.DeploymentStatus{model.ID: attempts[0].Status}, map[string][]app.Domain{model.ID: domains})
	screen := tui.NewAppListScreen(view)
	rows := screen.Rows()
	if len(rows) != 1 {
		t.Fatalf("Rows = %#v", rows)
	}
	if rows[0].Name != "my-app" || rows[0].Status != "healthy" || rows[0].DomainCount != 1 {
		t.Fatalf("app list row = %#v", rows[0])
	}

	failure := errors.New("fake build failed")
	runner.DeployErr = compose.NewDeployError(compose.DeployStageBuildServices, failure)
	if err := runAttempt("dep_failed"); !errors.Is(err, failure) {
		t.Fatalf("failed RedeployCurrentMain error = %v", err)
	}
	attempts, err = sqlite.ListDeploymentsByApp(ctx, model.ID)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("attempts after failure = %#v, error = %v", attempts, err)
	}
	var failed app.Deployment
	for _, attempt := range attempts {
		if attempt.ID == "dep_failed" {
			failed = attempt
		}
	}
	if failed.Status != app.DeploymentStatusFailed || failed.FailureStage != "build services" {
		t.Fatalf("failed attempt = %#v", failed)
	}
	release, err := sqlite.GetReleaseByAppCommit(ctx, model.ID, "abc123")
	if err != nil || release.Status != app.ReleaseStatusSucceeded {
		t.Fatalf("successful release after retry failure = %#v, error = %v", release, err)
	}
}

type smokeWorktreeCheckout struct{}

func (smokeWorktreeCheckout) Checkout(context.Context, string, string, string) error { return nil }

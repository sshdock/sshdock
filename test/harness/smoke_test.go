package harness

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/iketiunn/rumbase/internal/app"
	"github.com/iketiunn/rumbase/internal/compose"
	"github.com/iketiunn/rumbase/internal/router"
	"github.com/iketiunn/rumbase/internal/store"
	"github.com/iketiunn/rumbase/internal/tui"
)

func TestSmokeVersionCommands(t *testing.T) {
	root := filepath.Join("..", "..")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "rhumbase version",
			args: []string{"run", "./cmd/rhumbase", "version"},
			want: "rhumbase dev\n",
		},
		{
			name: "rhumbased version",
			args: []string{"run", "./cmd/rhumbased", "version"},
			want: "rhumbased dev\n",
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
	sqlite, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "rhumbase.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlite.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	service := app.NewService(sqlite, app.WithClock(func() time.Time { return now }))
	model, err := service.CreateApp(ctx, app.App{
		ID:     "app_1",
		Name:   "my-app",
		NodeID: "local",
		Status: app.AppStatusHealthy,
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	projectDir := filepath.Join("..", "fixtures", "compose", "valid")
	composePath, err := compose.DetectFile(projectDir)
	if err != nil {
		t.Fatalf("DetectFile: %v", err)
	}
	validation, err := compose.ValidateFile(composePath)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if len(validation.Services) == 0 {
		t.Fatal("expected at least one service")
	}

	release, err := service.CreateRelease(ctx, app.Release{
		ID:          "rel_1",
		AppID:       model.ID,
		CommitSHA:   "abc123",
		ComposePath: composePath,
	})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	deployment, err := service.StartDeployment(ctx, app.Deployment{
		ID:        "dep_1",
		AppID:     model.ID,
		ReleaseID: release.ID,
	})
	if err != nil {
		t.Fatalf("StartDeployment: %v", err)
	}

	runner := &compose.FakeRunner{}
	if err := runner.Deploy(ctx, compose.DeployRequest{AppName: model.Name, ReleaseID: release.ID, CommitSHA: release.CommitSHA, ComposePath: release.ComposePath}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if err := service.MarkDeploymentSucceeded(ctx, deployment.ID); err != nil {
		t.Fatalf("MarkDeploymentSucceeded: %v", err)
	}

	domain, err := service.AttachDomain(ctx, app.Domain{
		ID:          "dom_1",
		AppID:       model.ID,
		ServiceName: "web",
		DomainName:  "example.com",
		Port:        3000,
		HTTPS:       true,
	})
	if err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	fakeRouter := router.NewFakeRouter()
	if err := fakeRouter.AttachDomain(ctx, router.Route{
		AppID:       domain.AppID,
		ServiceName: domain.ServiceName,
		DomainName:  domain.DomainName,
		Port:        domain.Port,
		HTTPS:       domain.HTTPS,
	}); err != nil {
		t.Fatalf("router AttachDomain: %v", err)
	}

	apps, err := service.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	view := tui.NewAppListView(apps, map[string]app.Release{model.ID: {ID: release.ID, Status: app.ReleaseStatusSucceeded}}, map[string][]app.Domain{model.ID: {domain}})
	screen := tui.NewAppListScreen(view)
	rows := screen.Rows()
	if len(rows) != 1 {
		t.Fatalf("Rows = %#v", rows)
	}
	if rows[0].Name != "my-app" || rows[0].Status != "healthy" || rows[0].DomainCount != 1 {
		t.Fatalf("app list row = %#v", rows[0])
	}
}

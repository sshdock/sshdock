package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestStoreBackendDomainsCheckReportsCaddyUnavailableWithRemediation(t *testing.T) {
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	model := app.App{ID: "my-app", Name: "my-app", NodeID: "node-a", Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}
	if err := sqlite.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := sqlite.AttachDomain(ctx, app.Domain{
		ID:          "dom_my_app_example_com",
		AppID:       "my-app",
		ServiceName: "web",
		DomainName:  "my-app.example.com",
		Port:        3000,
		HTTPS:       true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{
		NodeID: "node-a",
		Router: &fakeRoutePublisher{RoutesErr: errors.New("connect: connection refused")},
		Now:    func() time.Time { return now },
	})
	cliRunner := NewRunner(backend, "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := cliRunner.Run([]string{"domains", "check", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains check exit code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"my-app.example.com\tweb\t3000\ttrue\tunavailable",
		"connection refused",
		"sudo sshdock diagnostics",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("domains check stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

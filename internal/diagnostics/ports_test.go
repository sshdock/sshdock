package diagnostics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestRunHTTPSListenerFollowsStoredRoutes(t *testing.T) {
	tests := []struct {
		name    string
		domains []app.Domain
		wantOK  bool
	}{
		{name: "fresh host", wantOK: true},
		{name: "HTTP only", domains: []app.Domain{{DomainName: "http://app.example.com"}}, wantOK: true},
		{name: "HTTP address overrides HTTPS metadata", domains: []app.Domain{{DomainName: "http://app.example.com", HTTPS: true}}, wantOK: true},
		{name: "bare HTTPS hostname", domains: []app.Domain{{DomainName: "app.example.com", HTTPS: true}}},
		{name: "bare hostname without HTTPS metadata", domains: []app.Domain{{DomainName: "app.example.com"}}},
		{name: "explicit HTTPS address", domains: []app.Domain{{DomainName: "https://app.example.com", HTTPS: true}}},
		{name: "explicit HTTPS without metadata", domains: []app.Domain{{DomainName: "https://app.example.com"}}},
		{name: "mixed routes", domains: []app.Domain{{DomainName: "http://one.example.com"}, {DomainName: "two.example.com", HTTPS: true}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := diagnosticsConfig(t.TempDir())
			prepareHealthyDiagnosticsRuntime(t, cfg)
			db, err := store.OpenSQLite(context.Background(), cfg.SQLiteDBPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.CreateApp(context.Background(), app.App{ID: "my-app", Name: "my-app", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			for index, domain := range test.domains {
				domain.ID = fmt.Sprintf("domain_%d", index)
				domain.AppID = "my-app"
				domain.ServiceName = "web"
				domain.Port = 3000
				if err := db.AttachDomain(context.Background(), domain); err != nil {
					t.Fatal(err)
				}
			}
			executor := healthyExecutor(cfg)
			executor.Outputs["ss -ltn"] = "LISTEN 0 4096 *:22 *:*\nLISTEN 0 4096 *:80 *:*\n"
			report := Run(context.Background(), cfg, executor)
			if report.OK != test.wantOK {
				t.Fatalf("report OK = %t, want %t:\n%s", report.OK, test.wantOK, report.String())
			}
			want := "fail port 443: not listening"
			if test.wantOK {
				want = "warn port 443: not listening; no HTTPS app routes configured"
			}
			if !strings.Contains(report.String(), want) {
				t.Fatalf("missing %q:\n%s", want, report.String())
			}
		})
	}
}

func TestRunHTTPSListenerAttachDetachBoundary(t *testing.T) {
	ctx := context.Background()
	cfg := diagnosticsConfig(t.TempDir())
	prepareHealthyDiagnosticsRuntime(t, cfg)
	db, err := store.OpenSQLite(ctx, cfg.SQLiteDBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Unix(1700000000, 0).UTC()
	if err := db.CreateApp(ctx, app.App{ID: "my-app", Name: "my-app", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	executor := healthyExecutor(cfg)
	for _, step := range []struct {
		name      string
		attach    bool
		detach    bool
		listening bool
		wantOK    bool
	}{
		{name: "before first route", wantOK: true},
		{name: "attached route requires listener", attach: true},
		{name: "HTTPS listener restored", listening: true, wantOK: true},
		{name: "last route detached", detach: true, wantOK: true},
	} {
		t.Run(step.name, func(t *testing.T) {
			if step.attach {
				if err := db.AttachDomain(ctx, app.Domain{ID: "domain_1", AppID: "my-app", ServiceName: "web", DomainName: "app.example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatal(err)
				}
			}
			if step.detach {
				if _, err := db.DeleteDomainByAppAndName(ctx, "my-app", "app.example.com"); err != nil {
					t.Fatal(err)
				}
			}
			executor.Outputs["ss -ltn"] = "LISTEN 0 4096 *:22 *:*\nLISTEN 0 4096 *:80 *:*\n"
			if step.listening {
				executor.Outputs["ss -ltn"] += "LISTEN 0 4096 *:443 *:*\n"
			}
			report := Run(ctx, cfg, executor)
			if report.OK != step.wantOK {
				t.Fatalf("report OK = %t, want %t:\n%s", report.OK, step.wantOK, report.String())
			}
		})
	}
}

func TestRunListenerFailuresRemainFailuresWithoutHTTPSRoutes(t *testing.T) {
	for _, test := range []struct {
		name      string
		listeners string
		ssErr     error
		want      string
	}{
		{name: "missing SSH", listeners: "LISTEN 0 4096 *:80 *:*\n", want: "fail port 22: not listening"},
		{name: "missing HTTP", listeners: "LISTEN 0 4096 *:22 *:*\n", want: "fail port 80: not listening"},
		{name: "listener command failed", ssErr: errors.New("ss unavailable"), want: "fail port 443: ss unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := diagnosticsConfig(t.TempDir())
			prepareHealthyDiagnosticsRuntime(t, cfg)
			executor := healthyExecutor(cfg)
			executor.Outputs["ss -ltn"] = test.listeners
			executor.Errs = map[string]error{"ss -ltn": test.ssErr}
			report := Run(context.Background(), cfg, executor)
			if report.OK || !strings.Contains(report.String(), test.want) {
				t.Fatalf("expected %q:\n%s", test.want, report.String())
			}
			if strings.Contains(report.String(), "firewall") {
				t.Fatalf("local listener failure incorrectly diagnoses firewall rules:\n%s", report.String())
			}
		})
	}
}

func TestRunHTTPSRequirementFailsClosedOnStoreErrors(t *testing.T) {
	for _, failure := range []string{"open", "routes query", "server config query"} {
		t.Run(failure, func(t *testing.T) {
			cfg := diagnosticsConfig(t.TempDir())
			prepareHealthyDiagnosticsRuntime(t, cfg)
			want := "fail sqlite migrations:"
			if failure == "open" {
				cfg.SQLiteDBPath = cfg.AppsDir
			} else {
				db, err := sql.Open("sqlite", cfg.SQLiteDBPath)
				if err != nil {
					t.Fatal(err)
				}
				statement := "ALTER TABLE domains RENAME COLUMN https TO broken_https"
				want = "fail app routes:"
				if failure == "server config query" {
					statement = "ALTER TABLE server_config RENAME COLUMN value TO broken_value"
					want = "fail server config:"
				}
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			executor := healthyExecutor(cfg)
			executor.Outputs["ss -ltn"] = "LISTEN 0 4096 *:22 *:*\nLISTEN 0 4096 *:80 *:*\n"
			report := Run(context.Background(), cfg, executor)
			if report.OK || !strings.Contains(report.String(), want) || !strings.Contains(report.String(), "fail port 443: not listening") {
				t.Fatalf("store failure weakened HTTPS readiness:\n%s", report.String())
			}
		})
	}
}

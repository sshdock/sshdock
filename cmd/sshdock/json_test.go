package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestJSONCommandsUsePersistentStore(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "sshdock.db")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", dbPath)
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	if err := db.CreateApp(ctx, app.App{ID: "persistent", Name: "persistent", NodeID: "node-json", Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueueDeployment(ctx, app.Deployment{ID: "dep_json", AppID: "persistent", CommitSHA: "commit-json", Trigger: app.DeploymentTriggerPush, StartedAt: now}, ""); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"apps", "list", "--json"},
		{"apps", "info", "persistent", "--json"},
		{"deployments", "list", "persistent", "--json"},
		{"deployments", "info", "persistent", "dep_json", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runWithEnv(args, &stdout, &stderr); code != 0 {
			t.Fatalf("%v: exit=%d stderr=%s stdout=%s", args, code, &stderr, &stdout)
		}
		var envelope struct {
			SchemaVersion string          `json:"schema_version"`
			Data          json.RawMessage `json:"data"`
			Error         json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.SchemaVersion != "1" || string(envelope.Error) != "null" || !strings.Contains(string(envelope.Data), "persistent") || stderr.Len() != 0 {
			t.Fatalf("%v: stdout=%s stderr=%s", args, &stdout, &stderr)
		}
	}
}

func TestJSONInitializationAndUsageErrors(t *testing.T) {
	for _, test := range []struct {
		name, host, errorCode string
		args                  []string
		wantCode              int
	}{
		{"config failure", " ", "unavailable", []string{"apps", "list", "--json"}, 1},
		{"mutation rejected", "server", "invalid_arguments", []string{"apps", "remove", "my-app", "--force", "--json"}, 2},
		{"missing app", "server", "not_found", []string{"apps", "info", "missing", "--json"}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			dataDir := filepath.Join(t.TempDir(), "data")
			t.Setenv("SSHDOCK_DATA_DIR", dataDir)
			t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(dataDir, "sshdock.db"))
			t.Setenv("SSHDOCK_GIT_HOST", test.host)
			var stdout, stderr bytes.Buffer
			if code := runWithEnv(test.args, &stdout, &stderr); code != test.wantCode {
				t.Fatalf("exit=%d want=%d stdout=%s stderr=%s", code, test.wantCode, &stdout, &stderr)
			}
			var envelope struct {
				Data  json.RawMessage `json:"data"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error.Code != test.errorCode || string(envelope.Data) != "null" || stderr.Len() != 0 {
				t.Fatalf("stdout=%s stderr=%s", &stdout, &stderr)
			}
			if test.wantCode == 2 {
				if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
					t.Fatalf("invalid JSON mutation initialized state: %v", err)
				}
			}
		})
	}
}

func TestJSONDiagnosticsRetainsFailedExit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SSHDOCK_DATA_DIR", t.TempDir())
	t.Setenv("SSHDOCK_GIT_HOST", " ")
	var stdout, stderr bytes.Buffer
	if code := runWithEnv([]string{"diagnostics", "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	var envelope struct {
		Data struct {
			OK     bool              `json:"ok"`
			Checks []json.RawMessage `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.OK || len(envelope.Data.Checks) == 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%s stderr=%s", &stdout, &stderr)
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperatorJSONAllowlist(t *testing.T) {
	for _, command := range []string{
		"apps list --json", "apps info app --json", "apps health app --json",
		"apps check app --json",
		"releases list app --json", "deployments list app --json", "deployments info app dep_1 --json",
		"domains list app --json", "domains check app --json", "events list app --json",
		"deployments info app dep_1", "apps exec app web -- printf --json",
	} {
		args, err := operatorOriginalCommandArgs(command)
		if err != nil || strings.Join(args, " ") != command {
			t.Fatalf("%q: args=%v err=%v", command, args, err)
		}
	}
	for _, command := range []string{
		"diagnostics --json", "apps remove app --force --json", "config get app SECRET --json",
		"apps --json list", "apps list --json --json", "apps list --json; id",
	} {
		if _, err := operatorOriginalCommandArgs(command); err == nil {
			t.Fatalf("accepted unsupported command %q", command)
		}
	}
}

func TestOperatorPreflightMissingStoreDoesNotInitializeState(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "absent")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(dataDir, "sshdock.db"))
	t.Setenv("SSH_ORIGINAL_COMMAND", "apps check missing --json")
	var stdout, stderr bytes.Buffer
	if code := runDashboard(nil, &stdout, &stderr); code != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	var result struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error.Code != "unavailable" || stderr.Len() != 0 {
		t.Fatalf("stdout=%s stderr=%s", &stdout, &stderr)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("restricted check initialized state: %v", err)
	}
}

func TestOperatorJSONDispatchAndEarlyErrors(t *testing.T) {
	for _, test := range []struct {
		name, command, host, errorCode string
		wantCode                       int
	}{
		{"empty apps", "apps list --json", "server", "", 0},
		{"missing app", "apps info missing --json", "server", "not_found", 1},
		{"config failure", "apps list --json", " ", "unavailable", 1},
		{"reject mutation", "apps remove app --force --json", "server", "invalid_arguments", 2},
		{"reject host diagnostics", "diagnostics --json", "server", "invalid_arguments", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			dataDir := filepath.Join(t.TempDir(), "data")
			t.Setenv("SSHDOCK_DATA_DIR", dataDir)
			t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(dataDir, "sshdock.db"))
			t.Setenv("SSHDOCK_GIT_HOST", test.host)
			t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
			t.Setenv("SSH_ORIGINAL_COMMAND", test.command)
			var stdout, stderr bytes.Buffer
			if code := runDashboard(nil, &stdout, &stderr); code != test.wantCode {
				t.Fatalf("exit=%d want=%d stdout=%s stderr=%s", code, test.wantCode, &stdout, &stderr)
			}
			var envelope struct {
				SchemaVersion string          `json:"schema_version"`
				Data          json.RawMessage `json:"data"`
				Error         *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.SchemaVersion != "1" || stderr.Len() != 0 {
				t.Fatalf("stdout=%s stderr=%s", &stdout, &stderr)
			}
			if test.errorCode == "" {
				if string(envelope.Data) != "[]" || envelope.Error != nil {
					t.Fatalf("stdout=%s", &stdout)
				}
			} else if envelope.Error == nil || envelope.Error.Code != test.errorCode || string(envelope.Data) != "null" {
				t.Fatalf("stdout=%s", &stdout)
			}
			if test.wantCode == 2 {
				if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
					t.Fatalf("rejected command initialized state: %v", err)
				}
			}
		})
	}
}

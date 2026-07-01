package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(version) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	want := "rhumbase dev\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunWithEnvPersistsAppAcrossInvocations(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("RHUMBASE_DATA_DIR", dataDir)
	t.Setenv("RHUMBASE_SQLITE_DB_PATH", filepath.Join(dataDir, "rhumbase.db"))
	t.Setenv("RHUMBASE_APPS_DIR", filepath.Join(dataDir, "apps"))
	t.Setenv("RHUMBASE_NODE_ID", "node-a")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runWithEnv([]string{"apps", "create", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps create exit code = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithEnv([]string{"apps", "list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps list exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "my-app\tcreated\tnode-a") {
		t.Fatalf("apps list stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithEnv([]string{"apps", "info", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps info exit code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"name: my-app", "status: created", "node: node-a"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("apps info stdout missing %q:\n%s", want, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithEnv([]string{"domains", "attach", "my-app", "web", "example.com", "--port", "3000"}, &stdout, &stderr); code != 0 {
		t.Fatalf("domains attach exit code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunWithEnvUsageDoesNotOpenStore(t *testing.T) {
	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("RHUMBASE_DATA_DIR", filepath.Join(blockingFile, "data"))
	t.Setenv("RHUMBASE_SQLITE_DB_PATH", filepath.Join(blockingFile, "data", "rhumbase.db"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithEnv([]string{"unknown"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage: rhumbase") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

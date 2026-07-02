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
	fakeBinDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(fakeBinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll fake bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeBinDir, "caddy"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile fake caddy: %v", err)
	}
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RHUMBASE_DATA_DIR", dataDir)
	t.Setenv("RHUMBASE_SQLITE_DB_PATH", filepath.Join(dataDir, "rhumbase.db"))
	t.Setenv("RHUMBASE_APPS_DIR", filepath.Join(dataDir, "apps"))
	t.Setenv("RHUMBASE_NODE_ID", "node-a")
	t.Setenv("RHUMBASE_GIT_HOST", "rhumbase.example.com")
	t.Setenv("RHUMBASE_CADDY_CONFIG_PATH", filepath.Join(dataDir, "Caddyfile"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runWithEnv([]string{"apps", "create", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps create exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "git remote add rhumbase git@rhumbase.example.com:my-app.git") {
		t.Fatalf("apps create stdout = %q", stdout.String())
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

func TestRunWithEnvUsesPersistedServerDomainForCreatedAppRemote(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("RHUMBASE_DATA_DIR", dataDir)
	t.Setenv("RHUMBASE_SQLITE_DB_PATH", filepath.Join(dataDir, "rhumbase.db"))
	t.Setenv("RHUMBASE_APPS_DIR", filepath.Join(dataDir, "apps"))
	t.Setenv("RHUMBASE_GIT_HOST", "env.example.com")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runWithEnv([]string{"server", "domain", "set", "rhumbase.example.com"}, &stdout, &stderr); code != 0 {
		t.Fatalf("server domain set exit code = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithEnv([]string{"apps", "create", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps create exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "git remote add rhumbase git@rhumbase.example.com:my-app.git") {
		t.Fatalf("apps create stdout = %q", stdout.String())
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

func TestCommandNeedsStoreForRecoveryCommands(t *testing.T) {
	tests := [][]string{
		{"apps", "restart", "my-app"},
		{"apps", "restart", "my-app", "web"},
		{"apps", "redeploy", "my-app"},
		{"apps", "rollback", "my-app", "rel_1"},
	}

	for _, args := range tests {
		if !commandNeedsStore(args) {
			t.Fatalf("commandNeedsStore(%v) = false, want true", args)
		}
	}
}

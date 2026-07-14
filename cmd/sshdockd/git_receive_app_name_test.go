package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/store"
)

func TestRunGitReceiveUsesPersistedControlHostInInvalidNameGuidance(t *testing.T) {
	// Given
	ctx := context.Background()
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "sshdock.db")
	t.Setenv("SSH_ORIGINAL_COMMAND", "git-receive-pack 'My_App.git'")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", dbPath)
	t.Setenv("SSHDOCK_GIT_HOST", "env.example.com")

	sqlite, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := sqlite.SetServerConfig(ctx, store.ServerConfig{
		BaseDomain: "example.com",
		GitHost:    "sshdock.example.com",
		UpdatedAt:  time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	code := runWithInput([]string{"git-receive"}, strings.NewReader(""), &stdout, &stderr)

	// Then
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	want := "git remote set-url sshdock git@sshdock.example.com:my-app.git"
	if !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

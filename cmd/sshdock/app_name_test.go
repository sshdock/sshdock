package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWithEnvRejectsInvalidAppCreationBeforeInitializingState(t *testing.T) {
	for _, name := range []string{"My_App", "my.app", "my app", "-app", "app-", strings.Repeat("a", 64), "應用", "../app"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("SSHDOCK_DATA_DIR", filepath.Join(root, "data"))
			t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(root, "external", "sshdock.db"))
			t.Setenv("SSHDOCK_GIT_HOST", "env.example.com")
			var stdout, stderr bytes.Buffer
			if code := runWithEnv([]string{"apps", "create", name}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "normalized DNS label") {
				t.Fatalf("apps create = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if name == "My_App" && !strings.Contains(stderr.String(), "git remote set-url sshdock git@env.example.com:my-app.git") {
				t.Fatalf("configured host guidance = %q", stderr.String())
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatalf("invalid app initialized data/database paths: %v, %v", entries, err)
			}
		})
	}
}

func TestRunWithEnvInvalidAppUsesExistingDatabaseReadOnlyForGuidance(t *testing.T) {
	for _, test := range []struct{ name, baseDomain, gitHost, wantHost string }{
		{name: "unrelated schema", wantHost: "env.example.com"},
		{name: "persisted Git host", gitHost: "persisted.example.com", wantHost: "persisted.example.com"},
		{name: "persisted control host", baseDomain: "example.com", gitHost: "ignored.example.com", wantHost: "sshdock.example.com"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			dbPath := filepath.Join(root, "existing ?state.db")
			dsn := url.URL{Scheme: "file", Path: dbPath}
			seed, err := sql.Open("sqlite", dsn.String())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := seed.ExecContext(context.Background(), `create table proof(value text); insert into proof values('retained')`); err != nil {
				t.Fatal(err)
			}
			if test.baseDomain != "" || test.gitHost != "" {
				if _, err := seed.ExecContext(context.Background(), `create table server_config(key text primary key,value text,updated_at text); insert into server_config values('base_domain',?,'2026-10-27T00:00:00Z'),('git_host',?,'2026-10-27T00:00:00Z')`, test.baseDomain, test.gitHost); err != nil {
					t.Fatal(err)
				}
			}
			if err := seed.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("SSHDOCK_DATA_DIR", filepath.Join(root, "absent-data"))
			t.Setenv("SSHDOCK_SQLITE_DB_PATH", dbPath)
			t.Setenv("SSHDOCK_GIT_HOST", "env.example.com")
			var stdout, stderr bytes.Buffer
			if code := runWithEnv([]string{"apps", "create", "My_App"}, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
				t.Fatalf("apps create = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "git remote set-url sshdock git@"+test.wantHost+":my-app.git") {
				t.Fatalf("persisted host guidance = %q", stderr.String())
			}
			after, err := os.ReadFile(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			afterInfo, err := os.Stat(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || info.Mode() != afterInfo.Mode() || !info.ModTime().Equal(afterInfo.ModTime()) {
				t.Fatal("app-name rejection changed existing database or schema")
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(dbPath) {
				t.Fatalf("app-name rejection initialized state or side files: %v, %v", entries, err)
			}
		})
	}
}

func TestRunWithEnvKeepsInvalidAppCreateUsageAndJSONContracts(t *testing.T) {
	for _, structured := range []bool{false, true} {
		t.Run(map[bool]string{false: "extra argument", true: "unsupported JSON"}[structured], func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("SSHDOCK_DATA_DIR", filepath.Join(root, "data"))
			t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(root, "external", "sshdock.db"))
			args := []string{"apps", "create", "My_App", "extra"}
			if structured {
				args[3] = "--json"
			}
			var stdout, stderr bytes.Buffer
			if code := runWithEnv(args, &stdout, &stderr); code != 2 {
				t.Fatalf("invalid syntax = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if structured {
				var envelope struct {
					Data  json.RawMessage       `json:"data"`
					Error struct{ Code string } `json:"error"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope.Error.Code != "invalid_arguments" || string(envelope.Data) != "null" || stderr.Len() != 0 {
					t.Fatalf("JSON contract = %s, %q, %v", stdout.String(), stderr.String(), err)
				}
			} else if stdout.Len() != 0 || !strings.Contains(stderr.String(), "invalid apps command") {
				t.Fatalf("usage contract stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatalf("invalid syntax initialized state: %v, %v", entries, err)
			}
		})
	}
}

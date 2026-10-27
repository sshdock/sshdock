package main

import (
	"bytes"
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGitReceiveRejectsBeforeCreatingDataOrExternalDatabaseDirectory(t *testing.T) {
	for _, command := range []string{"git-receive-pack 'My_App.git'", "git-receive-pack '../app.git'", "git-upload-pack 'app.git'"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("SSH_ORIGINAL_COMMAND", command)
			t.Setenv("SSHDOCK_DATA_DIR", filepath.Join(root, "data"))
			t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(root, "external", "sshdock.db"))
			t.Setenv("SSHDOCK_GIT_HOST", "env.example.com")
			var stdout, stderr bytes.Buffer
			if code := runWithInput([]string{"git-receive"}, rejectedGitInput{t}, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
				t.Fatalf("git-receive = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if command == "git-receive-pack 'My_App.git'" && !strings.Contains(stderr.String(), "git remote set-url sshdock git@env.example.com:my-app.git") {
				t.Fatalf("missing configured-host guidance: %q", stderr.String())
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatalf("rejected command created data/database paths: %v, %v", entries, err)
			}
		})
	}
}

func TestRunGitReceiveUsesExistingDatabaseOnlyForReadOnlyHostGuidance(t *testing.T) {
	for _, test := range []struct {
		name, baseDomain, gitHost, wantHost string
	}{
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
			t.Setenv("SSH_ORIGINAL_COMMAND", "git-receive-pack 'My_App.git'")
			t.Setenv("SSHDOCK_DATA_DIR", filepath.Join(root, "absent-data"))
			t.Setenv("SSHDOCK_SQLITE_DB_PATH", dbPath)
			t.Setenv("SSHDOCK_GIT_HOST", "env.example.com")
			var stdout, stderr bytes.Buffer
			if code := runWithInput([]string{"git-receive"}, rejectedGitInput{t}, &stdout, &stderr); code != 1 {
				t.Fatalf("git-receive = %d, stderr=%q", code, stderr.String())
			}
			want := "git remote set-url sshdock git@" + test.wantHost + ":my-app.git"
			if !strings.Contains(stderr.String(), want) || stdout.Len() != 0 {
				t.Fatalf("guidance stdout=%q stderr=%q, want %q", stdout.String(), stderr.String(), want)
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
				t.Fatal("name rejection changed existing database or schema")
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(dbPath) {
				t.Fatalf("name rejection initialized state or side files: %v, %v", entries, err)
			}
		})
	}
}

func TestRunGitHooksRejectInvalidAppBeforeReadingInputOrInitializingState(t *testing.T) {
	for _, command := range []string{"git-pre-receive", "git-hook"} {
		for _, name := range []string{"My_App", "my.app", "my app", "-app", "app-", strings.Repeat("a", 64), "應用"} {
			t.Run(command+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				t.Setenv("SSHDOCK_DATA_DIR", filepath.Join(root, "data"))
				t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(root, "external", "sshdock.db"))
				t.Setenv("SSHDOCK_GIT_HOST", "env.example.com")
				args := []string{command, "--app", name}
				if command == "git-hook" {
					args = append(args, "--repo", filepath.Join(root, "app", "repo.git"))
				}
				var stdout, stderr bytes.Buffer
				if code := runWithInput(args, rejectedGitInput{t}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "normalized DNS label") {
					t.Fatalf("%s = %d, stdout=%q stderr=%q", command, code, stdout.String(), stderr.String())
				}
				if strings.Contains(stderr.String(), "remote main updated") {
					t.Fatalf("invalid app reported an unread ref update: %q", stderr.String())
				}
				if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
					t.Fatalf("invalid hook app initialized state: %v, %v", entries, err)
				}
			})
		}
	}
}

type rejectedGitInput struct{ t *testing.T }

func (r rejectedGitInput) Read([]byte) (int, error) {
	r.t.Fatal("invalid app input must be rejected before reading stdin")
	return 0, nil
}

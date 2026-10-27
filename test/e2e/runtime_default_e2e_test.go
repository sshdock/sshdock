//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/store"
)

func TestOrdinaryBinariesUseDockerRuntimeEndToEnd(t *testing.T) {
	root, tmp := filepath.Join("..", ".."), t.TempDir()
	binDir, dataDir := filepath.Join(tmp, "bin"), filepath.Join(tmp, "data")
	worktree := filepath.Join(dataDir, "apps", "runtime-app", "worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "compose.yml"), []byte("services:\n  web:\n    image: example/web:latest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sshdock", "sshdockd"} {
		runCommand(t, root, nil, "go", "build", "-o", filepath.Join(binDir, name), "./cmd/"+name)
	}
	writeFakeCommand(t, binDir, "docker", "#!/bin/sh\nprintf 'native Docker rejected operation\\n' >&2\nexit 43\n")
	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, filepath.Join(dataDir, "sshdock.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	if err := db.CreateApp(ctx, app.App{ID: "runtime-app", Name: "runtime-app", NodeID: "local", WorktreePath: worktree, Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		binary string
		args   []string
	}{
		{"sshdock", []string{"apps", "start", "runtime-app"}},
		{"sshdockd", []string{"operator"}},
	} {
		t.Run(test.binary, func(t *testing.T) {
			command := exec.Command(filepath.Join(binDir, test.binary), test.args...)
			command.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"), "SSHDOCK_DATA_DIR="+dataDir, "SSH_ORIGINAL_COMMAND=apps start runtime-app")
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "native Docker rejected operation") || !strings.Contains(string(output), "apps redeploy runtime-app") {
				t.Fatalf("runtime command error=%v, output=%s", err, output)
			}
		})
	}
	events, err := db.ListEventsByApp(ctx, "runtime-app")
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, event := range events {
		if event.Type == "start.succeeded" {
			t.Fatal("Docker failure was recorded as success")
		}
		if event.Type == "start.failed" {
			failed++
		}
	}
	if failed != 2 {
		t.Fatalf("start.failed events=%d, want both ordinary binary operations", failed)
	}
}

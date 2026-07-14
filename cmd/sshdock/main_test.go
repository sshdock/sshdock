package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(version) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}

	want := "sshdock dev\n"
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
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(dataDir, "sshdock.db"))
	t.Setenv("SSHDOCK_APPS_DIR", filepath.Join(dataDir, "apps"))
	t.Setenv("SSHDOCK_NODE_ID", "node-a")
	t.Setenv("SSHDOCK_GIT_HOST", "sshdock.example.com")
	t.Setenv("SSHDOCK_CADDY_CONFIG_PATH", filepath.Join(dataDir, "Caddyfile"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runWithEnv([]string{"apps", "create", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps create exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "git remote add sshdock git@sshdock.example.com:my-app.git") {
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

func TestRunWithEnvUsesPersistedBaseDomainForCreatedAppRemote(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(dataDir, "sshdock.db"))
	t.Setenv("SSHDOCK_APPS_DIR", filepath.Join(dataDir, "apps"))
	t.Setenv("SSHDOCK_GIT_HOST", "env.example.com")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runWithEnv([]string{"server", "domain", "set", "example.com"}, &stdout, &stderr); code != 0 {
		t.Fatalf("server domain set exit code = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithEnv([]string{"apps", "create", "my-app"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apps create exit code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{
		"git remote add sshdock git@sshdock.example.com:my-app.git",
		"default URL after first deploy: https://my-app.example.com",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("apps create stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunWithEnvUsageDoesNotOpenStore(t *testing.T) {
	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("SSHDOCK_DATA_DIR", filepath.Join(blockingFile, "data"))
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(blockingFile, "data", "sshdock.db"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithEnv([]string{"unknown"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `Run "sshdock help" for available commands.`) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunWithEnvHelpDoesNotOpenStore(t *testing.T) {
	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("SSHDOCK_DATA_DIR", filepath.Join(blockingFile, "data"))
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(blockingFile, "data", "sshdock.db"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithEnv([]string{"config", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Config commands store encrypted app config.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunWithEnvDiagnosticsReportsConfigFailure(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_GIT_HOST", " ")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runWithEnv([]string{"diagnostics"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("diagnostics exit code = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "fail config") {
		t.Fatalf("diagnostics stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "SSHDOCK_GIT_HOST is required") {
		t.Fatalf("diagnostics stdout missing config error:\n%s", stdout.String())
	}
}

func TestRunWithEnvBackupCreateInspectAndRestore(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	caddyDir := filepath.Join(root, "caddy")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll data: %v", err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}
	writeFakeDockerVolumeCommand(t, filepath.Join(binDir, "docker"))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_SQLITE_DB_PATH", filepath.Join(dataDir, "sshdock.db"))
	t.Setenv("SSHDOCK_APPS_DIR", filepath.Join(dataDir, "apps"))
	t.Setenv("SSHDOCK_CONFIG_KEY_PATH", filepath.Join(dataDir, "config.key"))
	t.Setenv("SSHDOCK_GIT_AUTHORIZED_KEYS_PATH", filepath.Join(dataDir, "git", ".ssh", "authorized_keys"))
	t.Setenv("SSHDOCK_DASHBOARD_AUTHORIZED_KEYS_PATH", filepath.Join(dataDir, "dashboard", ".ssh", "authorized_keys"))
	t.Setenv("SSHDOCK_CADDY_CONFIG_PATH", filepath.Join(caddyDir, "sshdock.caddyfile"))
	t.Setenv("SSHDOCK_CADDY_MAIN_CONFIG_PATH", filepath.Join(caddyDir, "Caddyfile"))

	sqlite, err := store.OpenSQLite(ctx, filepath.Join(dataDir, "sshdock.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	if err := sqlite.CreateApp(ctx, app.App{
		ID:           "my-app",
		Name:         "my-app",
		NodeID:       "local",
		RepoPath:     filepath.Join(dataDir, "apps", "my-app", "repo.git"),
		WorktreePath: filepath.Join(dataDir, "apps", "my-app", "worktree"),
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	configService := appconfig.NewService(sqlite, filepath.Join(dataDir, "config.key"), appconfig.WithClock(func() time.Time { return now }))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte("postgres://secret")}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatalf("Close sqlite: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "git", ".ssh"), 0o755); err != nil {
		t.Fatalf("MkdirAll git ssh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "git", ".ssh", "authorized_keys"), []byte("ssh-ed25519 git-key\n"), 0o600); err != nil {
		t.Fatalf("WriteFile git authorized keys: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "dashboard", ".ssh"), 0o755); err != nil {
		t.Fatalf("MkdirAll dashboard ssh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "dashboard", ".ssh", "authorized_keys"), []byte("ssh-ed25519 dashboard-key\n"), 0o600); err != nil {
		t.Fatalf("WriteFile dashboard authorized keys: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "apps", "my-app", "worktree"), 0o755); err != nil {
		t.Fatalf("MkdirAll worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "apps", "my-app", "worktree", "compose.yml"), []byte("services:\n  web:\n    image: nginx:alpine\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	if err := os.MkdirAll(caddyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll caddy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caddyDir, "sshdock.caddyfile"), []byte("# generated\n"), 0o644); err != nil {
		t.Fatalf("WriteFile generated caddy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caddyDir, "Caddyfile"), []byte("import "+filepath.Join(caddyDir, "sshdock.caddyfile")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main caddy: %v", err)
	}

	archivePath := filepath.Join(root, "backup.tar.gz")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runWithEnv([]string{"backup", "create", "--output", archivePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("backup create exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "created backup ") || !strings.Contains(stdout.String(), "Docker volume inventory: 1") {
		t.Fatalf("backup create stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runWithEnv([]string{"backup", "inspect", archivePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("backup inspect exit code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"format: sshdock-backup/v1", "Docker volumes: 1", "sshdock_my_app_data"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("backup inspect stdout missing %q:\n%s", want, stdout.String())
		}
	}

	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatalf("RemoveAll data dir: %v", err)
	}
	if err := os.RemoveAll(caddyDir); err != nil {
		t.Fatalf("RemoveAll caddy dir: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runWithEnv([]string{"backup", "restore", archivePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("backup restore exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "restored backup ") {
		t.Fatalf("backup restore stdout = %q", stdout.String())
	}
	restoredStore, err := store.OpenSQLite(ctx, filepath.Join(dataDir, "sshdock.db"))
	if err != nil {
		t.Fatalf("OpenSQLite restored: %v", err)
	}
	defer restoredStore.Close()
	restoredConfig := appconfig.NewService(restoredStore, filepath.Join(dataDir, "config.key"))
	value, err := restoredConfig.Reveal(ctx, appconfig.ConfigRef{AppID: "my-app", Name: "DATABASE_URL"})
	if err != nil {
		t.Fatalf("Reveal restored config: %v", err)
	}
	if value != "postgres://secret" {
		t.Fatalf("restored config = %q", value)
	}
}

func TestCLIRunnerFromEnvDefaultsToDocker(t *testing.T) {
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "")

	runner, err := cliRunnerFromEnv()
	if err != nil {
		t.Fatalf("cliRunnerFromEnv: %v", err)
	}
	if got := fmt.Sprintf("%T", runner); got != "*compose.DockerRunner" {
		t.Fatalf("runner type = %s, want *compose.DockerRunner", got)
	}
}

func writeFakeDockerVolumeCommand(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
if [ "$1" = "volume" ] && [ "$2" = "ls" ]; then
  printf 'sshdock_my_app_data\n'
  exit 0
fi
if [ "$1" = "volume" ] && [ "$2" = "inspect" ]; then
  printf '[{"Name":"sshdock_my_app_data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/sshdock_my_app_data/_data"}]\n'
  exit 0
fi
echo "unexpected docker command: $*" >&2
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile fake docker: %v", err)
	}
}

func TestCLIRunnerFromEnvCanSelectFakeForTests(t *testing.T) {
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	t.Setenv("SSHDOCK_FAKE_COMPOSE_SERVICES", "web:running,worker:exited")

	runner, err := cliRunnerFromEnv()
	if err != nil {
		t.Fatalf("cliRunnerFromEnv: %v", err)
	}
	if got := fmt.Sprintf("%T", runner); got != "*compose.FakeRunner" {
		t.Fatalf("runner type = %s, want *compose.FakeRunner", got)
	}
	fake := runner.(*compose.FakeRunner)
	if len(fake.Services) != 2 || fake.Services[0].Name != "web" || fake.Services[0].State != "running" || fake.Services[1].Name != "worker" || fake.Services[1].State != "exited" {
		t.Fatalf("fake services = %#v", fake.Services)
	}
}

func TestCommandNeedsStoreForRecoveryCommands(t *testing.T) {
	tests := [][]string{
		{"apps", "health", "my-app"},
		{"apps", "restart", "my-app"},
		{"apps", "restart", "my-app", "web"},
		{"apps", "redeploy", "my-app"},
		{"apps", "rollback", "my-app", "rel_1"},
		{"deployments", "list", "my-app"},
	}

	for _, args := range tests {
		if !commandNeedsStore(args) {
			t.Fatalf("commandNeedsStore(%v) = false, want true", args)
		}
	}
}

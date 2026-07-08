package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/backup"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/store"
)

func TestBackupRestoreKeepsEncryptedConfigUsable(t *testing.T) {
	ctx := context.Background()
	source := backupHarnessConfig(t)
	if err := os.MkdirAll(source.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source data: %v", err)
	}

	sqlite, err := store.OpenSQLite(ctx, source.SQLiteDBPath)
	if err != nil {
		t.Fatalf("OpenSQLite source: %v", err)
	}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	if err := sqlite.CreateApp(ctx, app.App{
		ID:           "restore-app",
		Name:         "restore-app",
		NodeID:       "local",
		RepoPath:     source.AppRepoPath("restore-app"),
		WorktreePath: source.AppWorktreePath("restore-app"),
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	configService := appconfig.NewService(sqlite, source.ConfigKeyPath, appconfig.WithClock(func() time.Time { return now }))
	if err := configService.Set(ctx, appconfig.SetRequest{
		AppID: "restore-app",
		Name:  "DATABASE_URL",
		Value: []byte("postgres://restored-secret"),
	}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatalf("Close source store: %v", err)
	}

	writeHarnessFile(t, filepath.Join(source.AppRepoPath("restore-app"), "HEAD"), "ref: refs/heads/main\n", 0o644)
	writeHarnessFile(t, filepath.Join(source.AppWorktreePath("restore-app"), "compose.yml"), "services:\n  web:\n    image: nginx:alpine\n", 0o644)
	writeHarnessFile(t, source.GitAuthorizedKeysPath, "ssh-ed25519 git-key\n", 0o600)
	writeHarnessFile(t, source.DashboardAuthorizedKeysPath, "ssh-ed25519 dashboard-key\n", 0o600)
	writeHarnessFile(t, source.CaddyConfigPath, "# generated routes\n", 0o644)
	writeHarnessFile(t, source.CaddyMainConfigPath, "import "+source.CaddyConfigPath+"\n", 0o644)

	archivePath := filepath.Join(t.TempDir(), "sshdock-backup.tar.gz")
	createResult, err := backup.Create(ctx, backup.CreateRequest{
		Config:      source,
		Destination: archivePath,
		Now:         func() time.Time { return now },
		VolumeLister: harnessVolumeLister{volumes: []backup.Volume{{
			Name:   "sshdock_restore_app_data",
			Driver: "local",
		}}},
	})
	if err != nil {
		t.Fatalf("Create backup: %v", err)
	}
	if createResult.VolumeCount != 1 {
		t.Fatalf("backup volume count = %d, want 1", createResult.VolumeCount)
	}

	target := backupHarnessConfig(t)
	if err := backup.Restore(ctx, backup.RestoreRequest{Config: target, ArchivePath: archivePath}); err != nil {
		t.Fatalf("Restore backup: %v", err)
	}

	restoredStore, err := store.OpenSQLite(ctx, target.SQLiteDBPath)
	if err != nil {
		t.Fatalf("OpenSQLite restored: %v", err)
	}
	defer restoredStore.Close()
	restoredConfig := appconfig.NewService(restoredStore, target.ConfigKeyPath)
	value, err := restoredConfig.Reveal(ctx, appconfig.ConfigRef{AppID: "restore-app", Name: "DATABASE_URL"})
	if err != nil {
		t.Fatalf("Reveal restored config: %v", err)
	}
	if value != "postgres://restored-secret" {
		t.Fatalf("restored config value = %q", value)
	}

	generatedCaddy, err := os.ReadFile(target.CaddyConfigPath)
	if err != nil {
		t.Fatalf("Read restored Caddy config: %v", err)
	}
	if !strings.Contains(string(generatedCaddy), "generated routes") {
		t.Fatalf("restored Caddy config = %q", generatedCaddy)
	}

	inspection, err := backup.Inspect(ctx, archivePath)
	if err != nil {
		t.Fatalf("Inspect backup: %v", err)
	}
	if inspection.VolumeCount != 1 || inspection.Volumes[0].Name != "sshdock_restore_app_data" {
		t.Fatalf("volume inventory = %#v", inspection.Volumes)
	}
}

func backupHarnessConfig(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	caddyDir := filepath.Join(root, "caddy")
	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.SQLiteDBPath = filepath.Join(dataDir, "sshdock.db")
	cfg.AppsDir = filepath.Join(dataDir, "apps")
	cfg.ConfigKeyPath = filepath.Join(dataDir, "config.key")
	cfg.GitHomeDir = filepath.Join(dataDir, "git")
	cfg.GitAuthorizedKeysPath = filepath.Join(dataDir, "git", ".ssh", "authorized_keys")
	cfg.DashboardHostKeyPath = filepath.Join(dataDir, "dashboard", "ssh_host_rsa_key")
	cfg.DashboardAuthorizedKeysPath = filepath.Join(dataDir, "dashboard", ".ssh", "authorized_keys")
	cfg.CaddyConfigPath = filepath.Join(caddyDir, "sshdock.caddyfile")
	cfg.CaddyMainConfigPath = filepath.Join(caddyDir, "Caddyfile")
	return cfg
}

func writeHarnessFile(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

type harnessVolumeLister struct {
	volumes []backup.Volume
}

func (l harnessVolumeLister) ListVolumes(context.Context) ([]backup.Volume, error) {
	return append([]backup.Volume(nil), l.volumes...), nil
}

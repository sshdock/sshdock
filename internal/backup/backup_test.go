package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/store"
)

func TestCreateArchiveIncludesStateCaddyAndVolumeInventory(t *testing.T) {
	ctx := context.Background()
	cfg := backupTestConfig(t)
	writeBackupFixture(t, cfg)
	archivePath := filepath.Join(t.TempDir(), "sshdock-backup.tar.gz")
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)

	result, err := Create(ctx, CreateRequest{
		Config:      cfg,
		Destination: archivePath,
		Now:         func() time.Time { return now },
		VolumeLister: fakeVolumeLister{volumes: []Volume{{
			Name:       "sshdock_my_app_data",
			Driver:     "local",
			Mountpoint: "/var/lib/docker/volumes/sshdock_my_app_data/_data",
		}}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if result.Path != archivePath || result.VolumeCount != 1 || result.FileCount == 0 {
		t.Fatalf("Create result = %#v", result)
	}

	entries := readArchiveEntries(t, archivePath)
	for _, want := range []string{
		"manifest.json",
		"data/sshdock.db",
		"data/config.key",
		"data/apps/my-app/repo.git/HEAD",
		"data/apps/my-app/worktree/compose.yml",
		"data/git/.ssh/authorized_keys",
		"data/dashboard/.ssh/authorized_keys",
		"caddy/generated.caddyfile",
		"caddy/main.Caddyfile",
		"docker/volumes.json",
	} {
		if _, ok := entries[want]; !ok {
			t.Fatalf("archive missing %q; entries=%v", want, sortedKeys(entries))
		}
	}

	var manifest Manifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if manifest.FormatVersion != FormatVersion {
		t.Fatalf("manifest format = %q, want %q", manifest.FormatVersion, FormatVersion)
	}
	if !manifest.CreatedAt.Equal(now) {
		t.Fatalf("manifest created_at = %s, want %s", manifest.CreatedAt, now)
	}
	if len(manifest.RestoreGuardrails) == 0 || !strings.Contains(strings.Join(manifest.RestoreGuardrails, "\n"), "Stop sshdockd") {
		t.Fatalf("restore guardrails = %#v", manifest.RestoreGuardrails)
	}

	var inventory []Volume
	if err := json.Unmarshal(entries["docker/volumes.json"], &inventory); err != nil {
		t.Fatalf("unmarshal volume inventory: %v", err)
	}
	if len(inventory) != 1 || inventory[0].Name != "sshdock_my_app_data" {
		t.Fatalf("volume inventory = %#v", inventory)
	}
}

func TestCreateRejectsVolumeContentBackupFlag(t *testing.T) {
	ctx := context.Background()
	cfg := backupTestConfig(t)
	writeBackupFixture(t, cfg)
	archivePath := filepath.Join(t.TempDir(), "sshdock-backup.tar.gz")

	_, err := Create(ctx, CreateRequest{
		Config:         cfg,
		Destination:    archivePath,
		IncludeVolumes: true,
		VolumeLister:   fakeVolumeLister{},
	})
	if err == nil || !strings.Contains(err.Error(), "Docker volume content backup is not implemented") {
		t.Fatalf("Create error = %v, want explicit include-volumes rejection", err)
	}
	if _, statErr := os.Stat(archivePath); !os.IsNotExist(statErr) {
		t.Fatalf("archive created despite include-volumes rejection: %v", statErr)
	}
}

func TestInspectArchiveReportsManifestAndInventory(t *testing.T) {
	ctx := context.Background()
	cfg := backupTestConfig(t)
	writeBackupFixture(t, cfg)
	archivePath := filepath.Join(t.TempDir(), "sshdock-backup.tar.gz")
	if _, err := Create(ctx, CreateRequest{
		Config:      cfg,
		Destination: archivePath,
		VolumeLister: fakeVolumeLister{volumes: []Volume{{
			Name:   "sshdock_my_app_data",
			Driver: "local",
		}}},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	inspection, err := Inspect(ctx, archivePath)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if inspection.Manifest.FormatVersion != FormatVersion {
		t.Fatalf("format = %q", inspection.Manifest.FormatVersion)
	}
	if inspection.VolumeCount != 1 {
		t.Fatalf("volume count = %d, want 1", inspection.VolumeCount)
	}
	if inspection.FileCount == 0 {
		t.Fatalf("file count = 0")
	}
}

func TestRestoreValidatesArchiveBeforeMutatingTarget(t *testing.T) {
	ctx := context.Background()
	cfg := backupTestConfig(t)
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll data dir: %v", err)
	}
	sentinelPath := filepath.Join(cfg.DataDir, "sentinel")
	if err := os.WriteFile(sentinelPath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile sentinel: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "invalid.tar.gz")
	writeInvalidArchive(t, archivePath)

	err := Restore(ctx, RestoreRequest{Config: cfg, ArchivePath: archivePath})
	if err == nil || !strings.Contains(err.Error(), "missing required archive entry data/sshdock.db") {
		t.Fatalf("Restore error = %v, want missing database validation", err)
	}
	if data, readErr := os.ReadFile(sentinelPath); readErr != nil || string(data) != "keep" {
		t.Fatalf("sentinel mutated before validation: data=%q err=%v", data, readErr)
	}
}

func TestRestoreValidatesTargetPermissionsBeforeMutatingTarget(t *testing.T) {
	ctx := context.Background()
	source := backupTestConfig(t)
	writeBackupFixture(t, source)
	archivePath := filepath.Join(t.TempDir(), "sshdock-backup.tar.gz")
	if _, err := Create(ctx, CreateRequest{Config: source, Destination: archivePath, VolumeLister: fakeVolumeLister{}}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	target := backupTestConfig(t)
	if err := os.MkdirAll(target.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll target data dir: %v", err)
	}
	sentinelPath := filepath.Join(target.DataDir, "sentinel")
	if err := os.WriteFile(sentinelPath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile sentinel: %v", err)
	}
	if err := os.Chmod(target.DataDir, 0o777); err != nil {
		t.Fatalf("Chmod target data dir: %v", err)
	}

	err := Restore(ctx, RestoreRequest{Config: target, ArchivePath: archivePath})
	if err == nil || !strings.Contains(err.Error(), "fix ownership and permissions before restore") {
		t.Fatalf("Restore error = %v, want target permission validation", err)
	}
	if data, readErr := os.ReadFile(sentinelPath); readErr != nil || string(data) != "keep" {
		t.Fatalf("sentinel mutated before validation: data=%q err=%v", data, readErr)
	}
}

func TestRestoreKeepsConfigKeyUsableForEncryptedConfig(t *testing.T) {
	ctx := context.Background()
	source := backupTestConfig(t)
	writeBackupFixture(t, source)
	symlinkCreated := true
	if err := os.Symlink("compose.yml", filepath.Join(source.AppWorktreePath("my-app"), "compose-link.yml")); err != nil {
		t.Logf("skipping symlink assertion: %v", err)
		symlinkCreated = false
	}
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	sqlite, err := store.OpenSQLite(ctx, source.SQLiteDBPath)
	if err != nil {
		t.Fatalf("OpenSQLite source: %v", err)
	}
	if err := sqlite.CreateApp(ctx, app.App{
		ID:           "my-app",
		Name:         "my-app",
		NodeID:       "local",
		RepoPath:     source.AppRepoPath("my-app"),
		WorktreePath: source.AppWorktreePath("my-app"),
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	configService := appconfig.NewService(sqlite, source.ConfigKeyPath, appconfig.WithClock(func() time.Time { return now }))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte("postgres://secret")}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatalf("Close source: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "sshdock-backup.tar.gz")
	if _, err := Create(ctx, CreateRequest{Config: source, Destination: archivePath, VolumeLister: fakeVolumeLister{}}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	target := backupTestConfig(t)
	if err := Restore(ctx, RestoreRequest{Config: target, ArchivePath: archivePath}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	restoredStore, err := store.OpenSQLite(ctx, target.SQLiteDBPath)
	if err != nil {
		t.Fatalf("OpenSQLite restored: %v", err)
	}
	defer restoredStore.Close()
	restoredConfig := appconfig.NewService(restoredStore, target.ConfigKeyPath)
	value, err := restoredConfig.Reveal(ctx, appconfig.ConfigRef{AppID: "my-app", Name: "DATABASE_URL"})
	if err != nil {
		t.Fatalf("Reveal restored config: %v", err)
	}
	if value != "postgres://secret" {
		t.Fatalf("restored config = %q", value)
	}
	if symlinkCreated {
		linkTarget, err := os.Readlink(filepath.Join(target.AppWorktreePath("my-app"), "compose-link.yml"))
		if err != nil {
			t.Fatalf("Readlink restored worktree symlink: %v", err)
		}
		if linkTarget != "compose.yml" {
			t.Fatalf("restored symlink target = %q, want compose.yml", linkTarget)
		}
	}
}

type fakeVolumeLister struct {
	volumes []Volume
	err     error
}

func (f fakeVolumeLister) ListVolumes(context.Context) ([]Volume, error) {
	return append([]Volume(nil), f.volumes...), f.err
}

func backupTestConfig(t *testing.T) config.Config {
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

func writeBackupFixture(t *testing.T, cfg config.Config) {
	t.Helper()
	files := map[string]string{
		cfg.SQLiteDBPath:  "",
		cfg.ConfigKeyPath: strings.Repeat("k", 32),
		filepath.Join(cfg.AppRepoPath("my-app"), "HEAD"):            "ref: refs/heads/main\n",
		filepath.Join(cfg.AppWorktreePath("my-app"), "compose.yml"): "services:\n  web:\n    image: nginx:alpine\n",
		cfg.GitAuthorizedKeysPath:                                   "ssh-ed25519 git-key\n",
		cfg.DashboardAuthorizedKeysPath:                             "ssh-ed25519 dashboard-key\n",
		cfg.CaddyConfigPath:                                         "# generated routes\n",
		cfg.CaddyMainConfigPath:                                     "import " + cfg.CaddyConfigPath + "\n",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", path, err)
		}
		mode := os.FileMode(0o644)
		if path == cfg.ConfigKeyPath {
			mode = 0o600
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}
}

func readArchiveEntries(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open archive: %v", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("NewReader gzip: %v", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	entries := map[string][]byte{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next tar: %v", err)
		}
		if header.FileInfo().IsDir() {
			continue
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatalf("Read entry %s: %v", header.Name, err)
		}
		entries[header.Name] = data
	}
	return entries
}

func writeInvalidArchive(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create invalid archive: %v", err)
	}
	defer file.Close()
	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()
	data, err := json.Marshal(Manifest{FormatVersion: FormatVersion, CreatedAt: time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	if err := tarWriter.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o644, Size: int64(len(data))}); err != nil {
		t.Fatalf("WriteHeader manifest: %v", err)
	}
	if _, err := tarWriter.Write(data); err != nil {
		t.Fatalf("Write manifest: %v", err)
	}
}

func sortedKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

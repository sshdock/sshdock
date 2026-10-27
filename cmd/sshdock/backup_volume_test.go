package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/backup"
)

func TestVolumeInspectIsOfflineAndDoesNotInitializeHostState(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "absent", "data")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("PATH", root)
	archive := filepath.Join(root, "volume.tar.gz")
	writeCLIVolumeArchive(t, archive)
	var stdout, stderr bytes.Buffer
	code := runWithEnv([]string{"backup", "volume", "inspect", archive}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("inspect exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{backup.VolumeFormatVersion, "source volume: source-data", "files: 1", "bytes: 4", "unencrypted", "numeric UID/GID"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("inspection missing %q: %s", want, stdout.String())
		}
	}
	if _, err := os.Stat(filepath.Dir(dataDir)); !os.IsNotExist(err) {
		t.Fatalf("volume inspect initialized host state: %v", err)
	}
}

func TestVolumeCommandRequiresExplicitSelectionAndQuiescence(t *testing.T) {
	for _, args := range [][]string{
		{"export", "source-data", "--output", "new.tar.gz"},
		{"export", "source-data", "--quiesced"},
		{"export", "source-data", "--output", "new.tar.gz", "--quiesced", "extra"},
		{"restore", "archive.tar.gz"},
		{"restore", "archive.tar.gz", "--name", "new-data", "--force"},
		{"inspect", "archive.tar.gz", "extra"},
		{"unknown", "source-data"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())
			var stdout, stderr bytes.Buffer
			code := runWithEnv(append([]string{"backup", "volume"}, args...), &stdout, &stderr)
			if code != 2 || stderr.Len() == 0 || stdout.Len() != 0 || strings.Contains(stderr.String(), "executable file not found") {
				t.Fatalf("invalid arguments reached runtime: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestVolumeRestoreRejectsBadArchiveBeforeDockerOrHostState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", root)
	dataDir := filepath.Join(root, "absent", "data")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	archive := filepath.Join(root, "bad.tar.gz")
	if err := os.WriteFile(archive, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runWithEnv([]string{"backup", "volume", "restore", archive, "--name", "new-data"}, &stdout, &stderr)
	if code != 1 || stdout.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "executable file not found") {
		t.Fatalf("bad archive reached runtime: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Dir(dataDir)); !os.IsNotExist(err) {
		t.Fatalf("volume restore initialized host state: %v", err)
	}
}

func writeCLIVolumeArchive(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	manifest, err := json.Marshal(backup.VolumeManifest{FormatVersion: backup.VolumeFormatVersion, CreatedAt: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), SourceVolume: "source-data", Driver: "local"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		header tar.Header
		data   []byte
	}{
		{header: tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(manifest))}, data: manifest},
		{header: tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o700, Uid: 123, Gid: 456}},
		{header: tar.Header{Name: "data/hello.txt", Typeflag: tar.TypeReg, Mode: 0o640, Uid: 123, Gid: 456, Size: 4}, data: []byte{0, 'a', '\n', 0xff}},
	} {
		if err := tw.WriteHeader(&entry.header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	for _, closer := range []interface{ Close() error }{tw, gz, file} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

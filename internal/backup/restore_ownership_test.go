package backup

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractArchive_applies_data_entry_ownership_when_restoring(t *testing.T) {
	// Given an archive whose SSHDock data entries belong to the daemon user.
	archivePath := filepath.Join(t.TempDir(), "ownership.tar.gz")
	writeOwnershipArchive(t, archivePath)
	owners := map[string][2]int{}

	// When the archive is extracted with an ownership recorder.
	err := extractArchiveWithOwnership(archivePath, t.TempDir(), func(path string, uid int, gid int) error {
		owners[filepath.Base(path)] = [2]int{uid, gid}
		return nil
	})

	// Then each restored data entry receives its archived owner and group.
	if err != nil {
		t.Fatalf("extractArchiveWithOwnership: %v", err)
	}
	for name, want := range map[string][2]int{
		"data":       {1001, 1002},
		"sshdock.db": {1001, 1002},
	} {
		if got := owners[name]; got != want {
			t.Fatalf("ownership for %s = %#v, want %#v", name, got, want)
		}
	}
}

func writeOwnershipArchive(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create ownership archive: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	entries := []tar.Header{
		{Name: "data", Typeflag: tar.TypeDir, Mode: 0o755, Uid: 1001, Gid: 1002},
		{Name: "data/sshdock.db", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1, Uid: 1001, Gid: 1002},
	}
	for _, entry := range entries {
		if err := tarWriter.WriteHeader(&entry); err != nil {
			t.Fatalf("WriteHeader %s: %v", entry.Name, err)
		}
		if entry.Size > 0 {
			if _, err := tarWriter.Write([]byte("x")); err != nil {
				t.Fatalf("Write %s: %v", entry.Name, err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("Close tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("Close gzip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close ownership archive: %v", err)
	}
}

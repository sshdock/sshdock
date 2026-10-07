package backup

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractArchiveRejectsSymlinkChainEscape(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "extract")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	protected := filepath.Join(parent, "protected.txt")
	if err := os.WriteFile(protected, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(parent, "chain.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for _, header := range []tar.Header{
		{Name: "data", Typeflag: tar.TypeDir, Mode: 0o700},
		{Name: "data/up", Typeflag: tar.TypeSymlink, Linkname: ".."},
		{Name: "data/out", Typeflag: tar.TypeSymlink, Linkname: "up/.."},
		{Name: "data/out/protected.txt", Typeflag: tar.TypeReg, Mode: 0o600, Size: 7},
	} {
		if err := tw.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := tw.Write([]byte("changed")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractArchiveWithOwnership(archivePath, destination, func(string, int, int) error { return nil }); err == nil {
		t.Error("accepted a symlink chain escaping the extraction directory")
	}
	if data, err := os.ReadFile(protected); err != nil || string(data) != "original" {
		t.Fatalf("file outside extraction root changed: %q %v", data, err)
	}
}

package harness

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProjectBrandingIsSSHDock(t *testing.T) {
	root := repoRoot(t)

	for _, path := range []string{
		"cmd/sshdock/main.go",
		"cmd/sshdockd/main.go",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("expected branded path %s to exist: %v", path, err)
		}
	}

	assertFileContains(t, filepath.Join(root, "go.mod"), "module github.com/sshdock/sshdock")
	assertFileContains(t, filepath.Join(root, "Makefile"), "APP_NAME := sshdock")
	assertFileContains(t, filepath.Join(root, "Makefile"), "DAEMON_NAME := sshdockd")

	oldTokens := []string{
		"rhum" + "base",
		"Rhum" + "base",
		"RHUM" + "BASE",
		"rum" + "base",
	}
	for _, path := range repositoryFiles(t, root) {
		contents, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read repository file %s: %v", path, err)
		}
		for _, token := range oldTokens {
			if strings.Contains(string(contents), token) {
				t.Fatalf("old project token %q remains in %s", token, path)
			}
		}
	}
}

func TestRepositoryFilesReturnsRegularFilesOnly(t *testing.T) {
	// Given a repository containing a regular file and a symlink to a directory.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("sshdock"), 0o600); err != nil {
		t.Fatalf("write regular file: %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "external")); err != nil {
		t.Skipf("create directory symlink: %v", err)
	}

	// When repository files are discovered.
	files := repositoryFiles(t, root)

	// Then only the regular file is returned.
	if len(files) != 1 || files[0] != "tracked.txt" {
		t.Fatalf("repositoryFiles() = %v, want [tracked.txt]", files)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func repositoryFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", ".tmp", "_artifacts":
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository files: %v", err)
	}
	return files
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(contents), want) {
		t.Fatalf("%s does not contain %q", path, want)
	}
}

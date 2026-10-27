package deploycoord

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRouteLockUsesReadableEmptyFileAndReadOnlyExclusiveFlock(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	before, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(dir)
	first, err := manager.AcquireRoutes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if first.file != nil {
			_ = first.Release()
		}
	})
	info, err := first.file.Stat()
	if err != nil || info.Mode().Perm() != 0o644 || info.Size() != 0 {
		t.Fatalf("route lock = %#v, %v; want empty0644 file", info, err)
	}
	flags, err := unix.FcntlInt(first.file.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		t.Fatalf("route lock open flags = %x, %v; want read-only", flags, err)
	}
	if _, err := first.file.WriteString("not lock data"); err == nil {
		t.Fatal("read-only route lock accepted content")
	}
	blocked, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
	defer cancel()
	if _, err := NewManager(dir).AcquireRoutes(blocked); !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "routes lock") {
		t.Fatalf("competing route lock = %v, want named deadline error", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	first.file = nil
	if err := os.Chmod(filepath.Join(dir, "routes.lock"), 0o444); err != nil {
		t.Fatal(err)
	}
	second, err := NewManager(dir).AcquireRoutes(ctx)
	if err != nil {
		t.Fatalf("exclusive flock requires write access: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(dir)
	if err != nil || after.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("route lock changed configured directory permissions: before=%v after=%v error=%v", before.Mode(), after, err)
	}
}

func TestRouteLockDirectoryRejectsRootFirstCreationWithoutChangingExistingPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locks")
	if err := ensureRouteLockDirectory(path, 0); err == nil || !strings.Contains(err.Error(), "matching bootstrap") {
		t.Fatalf("root-created lock directory: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root-first check created a directory: %v", err)
	}
	if err := ensureRouteLockDirectory(path, 1000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ensureRouteLockDirectory(path, 0); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("existing directory mode changed: %#v, %v", info, err)
	}
}

func TestPrivateAppLockModeRemainsUnchanged(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	manager := NewManager(dir)
	appGuard, err := manager.AcquireApp(ctx, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	info, statErr := appGuard.file.Stat()
	flags, flagErr := unix.FcntlInt(appGuard.file.Fd(), unix.F_GETFL, 0)
	if statErr != nil || info.Mode().Perm() != 0o600 || flagErr != nil || flags&unix.O_ACCMODE != unix.O_RDWR {
		t.Errorf("private lock mode/flags changed: info=%v stat=%v flags=%x flags error=%v", info, statErr, flags, flagErr)
	}
	if err := appGuard.Release(); err != nil {
		t.Error(err)
	}
	info, err = os.Stat(filepath.Join(dir, "apps"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private app lock directory mode changed: %#v, %v", info, err)
	}
}

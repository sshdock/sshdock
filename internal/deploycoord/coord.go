package deploycoord

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

var ErrAppBusy = errors.New("app push is already running")

const lockRetryInterval = 25 * time.Millisecond

type Manager struct {
	dir string
}

type Guard struct {
	file *os.File
}

func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

func (m *Manager) AcquireApp(ctx context.Context, appName string) (*Guard, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire push lock for app %q: %w", appName, err)
	}

	path := filepath.Join(m.dir, "apps", appName+".lock")
	guard, err := acquire(path, unix.LOCK_EX|unix.LOCK_NB, false)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return nil, fmt.Errorf("another push is already running for app %q; wait for it to finish and try again: %w", appName, ErrAppBusy)
	}
	if err != nil {
		return nil, fmt.Errorf("acquire push lock for app %q: %w", appName, err)
	}
	return guard, nil
}

func (m *Manager) AcquireRoutes(ctx context.Context) (*Guard, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire routes lock: %w", err)
	}

	path := filepath.Join(m.dir, "routes.lock")
	guard, err := acquire(path, unix.LOCK_EX|unix.LOCK_NB, true)
	if err == nil {
		return guard, nil
	}
	if !errors.Is(err, unix.EWOULDBLOCK) {
		return nil, fmt.Errorf("acquire routes lock: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("wait for routes lock: %w", err)
	}
	retry := time.NewTicker(lockRetryInterval)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for routes lock: %w", ctx.Err())
		case <-retry.C:
		}

		guard, err = acquire(path, unix.LOCK_EX|unix.LOCK_NB, true)
		if errors.Is(err, unix.EWOULDBLOCK) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("wait for routes lock: %w", err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			releaseErr := guard.Release()
			return nil, errors.Join(fmt.Errorf("wait for routes lock: %w", ctxErr), releaseErr)
		}
		return guard, nil
	}
}

func acquire(path string, operation int, shared bool) (*Guard, error) {
	var file *os.File
	var err error
	if shared {
		if err := ensureRouteLockDirectory(filepath.Dir(path), os.Geteuid()); err != nil {
			return nil, err
		}
		file, err = openRouteLock(path)
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create lock directory: %w", err)
		}
		file, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	}
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), operation); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close unacquired lock file: %w", closeErr))
		}
		return nil, err
	}
	return &Guard{file: file}, nil
}

func ensureRouteLockDirectory(path string, euid int) error {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) && euid == 0 {
		return fmt.Errorf("routing lock directory %q does not exist; run the matching bootstrap to provision it for the daemon user", path)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect routing lock directory: %w", err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create routing lock directory: %w", err)
	}
	return nil
}

func openRouteLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return os.Open(path)
	}
	if err != nil {
		return nil, err
	}
	// Coordination files contain no data; both root and the daemon must be able to flock them.
	if err := file.Chmod(0o644); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func (g *Guard) Release() error {
	unlockErr := unix.Flock(int(g.file.Fd()), unix.LOCK_UN)
	closeErr := g.file.Close()
	if err := errors.Join(unlockErr, closeErr); err != nil {
		return fmt.Errorf("release file lock: %w", err)
	}
	return nil
}

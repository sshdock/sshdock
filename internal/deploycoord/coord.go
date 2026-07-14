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

const deploymentRetryInterval = 25 * time.Millisecond

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
	guard, err := acquire(path, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return nil, fmt.Errorf("another push is already running for app %q; wait for it to finish and try again: %w", appName, ErrAppBusy)
	}
	if err != nil {
		return nil, fmt.Errorf("acquire push lock for app %q: %w", appName, err)
	}
	return guard, nil
}

func (m *Manager) AcquireDeployment(ctx context.Context, notifyWait func() error) (*Guard, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire deployment lock: %w", err)
	}

	path := filepath.Join(m.dir, "deployment.lock")
	guard, err := acquire(path, unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return guard, nil
	}
	if !errors.Is(err, unix.EWOULDBLOCK) {
		return nil, fmt.Errorf("acquire deployment lock: %w", err)
	}
	if notifyWait != nil {
		if err := notifyWait(); err != nil {
			return nil, fmt.Errorf("report deployment wait: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("wait for deployment lock: %w", err)
	}
	retry := time.NewTicker(deploymentRetryInterval)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for deployment lock: %w", ctx.Err())
		case <-retry.C:
		}

		guard, err = acquire(path, unix.LOCK_EX|unix.LOCK_NB)
		if errors.Is(err, unix.EWOULDBLOCK) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("wait for deployment lock: %w", err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			releaseErr := guard.Release()
			return nil, errors.Join(fmt.Errorf("wait for deployment lock: %w", ctxErr), releaseErr)
		}
		return guard, nil
	}
}

func acquire(path string, operation int) (*Guard, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
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

func (g *Guard) Release() error {
	unlockErr := unix.Flock(int(g.file.Fd()), unix.LOCK_UN)
	closeErr := g.file.Close()
	if err := errors.Join(unlockErr, closeErr); err != nil {
		return fmt.Errorf("release file lock: %w", err)
	}
	return nil
}

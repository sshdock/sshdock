package deploycoord

import (
	"context"
	"errors"
	"testing"
	"time"
)

const coordinationTestTimeout = 2 * time.Second

func TestManagerSerializesRouteChanges(t *testing.T) {
	// Given
	manager := NewManager(t.TempDir())
	first, err := manager.AcquireRoutes(context.Background())
	if err != nil {
		t.Fatalf("first AcquireRoutes: %v", err)
	}
	secondAcquired := make(chan *Guard, 1)
	secondErr := make(chan error, 1)
	go func() {
		guard, acquireErr := manager.AcquireRoutes(context.Background())
		if acquireErr != nil {
			secondErr <- acquireErr
			return
		}
		secondAcquired <- guard
	}()
	// When
	select {
	case guard := <-secondAcquired:
		if releaseErr := guard.Release(); releaseErr != nil {
			t.Fatalf("release unexpectedly acquired second guard: %v", releaseErr)
		}
		t.Fatal("second route lock acquired before first released")
	case err := <-secondErr:
		t.Fatalf("second AcquireRoutes: %v", err)
	case <-time.After(2 * lockRetryInterval):
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release first route lock: %v", err)
	}

	// Then
	select {
	case err := <-secondErr:
		t.Fatalf("second AcquireRoutes: %v", err)
	case guard := <-secondAcquired:
		if err := guard.Release(); err != nil {
			t.Fatalf("release second route lock: %v", err)
		}
	case <-time.After(coordinationTestTimeout):
		t.Fatal("second route lock did not acquire after first released")
	}
}

func TestManagerStopsWaitingWhenContextIsCanceled(t *testing.T) {
	// Given
	manager := NewManager(t.TempDir())
	first, err := manager.AcquireRoutes(context.Background())
	if err != nil {
		t.Fatalf("first AcquireRoutes: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(2*lockRetryInterval, cancel)
	defer timer.Stop()

	// When
	_, err = manager.AcquireRoutes(ctx)

	// Then
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AcquireRoutes error = %v, want context canceled", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release first route lock: %v", err)
	}
	retry, err := manager.AcquireRoutes(context.Background())
	if err != nil {
		t.Fatalf("AcquireRoutes after canceled waiter: %v", err)
	}
	if err := retry.Release(); err != nil {
		t.Fatalf("release retry route lock: %v", err)
	}
}

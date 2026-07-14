package deploycoord

import (
	"context"
	"errors"
	"testing"
	"time"
)

const coordinationTestTimeout = 2 * time.Second

func TestManagerSerializesDeploymentsAndNotifiesWaiter(t *testing.T) {
	// Given
	manager := NewManager(t.TempDir())
	first, err := manager.AcquireDeployment(context.Background(), nil)
	if err != nil {
		t.Fatalf("first AcquireDeployment: %v", err)
	}
	waiting := make(chan struct{})
	secondAcquired := make(chan *Guard, 1)
	secondErr := make(chan error, 1)
	go func() {
		guard, acquireErr := manager.AcquireDeployment(context.Background(), func() error {
			close(waiting)
			return nil
		})
		if acquireErr != nil {
			secondErr <- acquireErr
			return
		}
		secondAcquired <- guard
	}()
	select {
	case <-waiting:
	case <-time.After(coordinationTestTimeout):
		t.Fatal("second deployment did not report waiting")
	}

	// When
	select {
	case guard := <-secondAcquired:
		if releaseErr := guard.Release(); releaseErr != nil {
			t.Fatalf("release unexpectedly acquired second guard: %v", releaseErr)
		}
		t.Fatal("second deployment acquired before first released")
	default:
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release first deployment: %v", err)
	}

	// Then
	select {
	case err := <-secondErr:
		t.Fatalf("second AcquireDeployment: %v", err)
	case guard := <-secondAcquired:
		if err := guard.Release(); err != nil {
			t.Fatalf("release second deployment: %v", err)
		}
	case <-time.After(coordinationTestTimeout):
		t.Fatal("second deployment did not acquire after first released")
	}
}

func TestManagerStopsWaitingWhenContextIsCanceled(t *testing.T) {
	// Given
	manager := NewManager(t.TempDir())
	first, err := manager.AcquireDeployment(context.Background(), nil)
	if err != nil {
		t.Fatalf("first AcquireDeployment: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, acquireErr := manager.AcquireDeployment(ctx, func() error {
			close(waiting)
			return nil
		})
		done <- acquireErr
	}()
	select {
	case <-waiting:
	case <-time.After(coordinationTestTimeout):
		t.Fatal("canceled deployment did not report waiting")
	}

	// When
	cancel()

	// Then
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("AcquireDeployment error = %v, want context canceled", err)
		}
	case <-time.After(coordinationTestTimeout):
		t.Fatal("AcquireDeployment did not stop after cancellation")
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release first deployment: %v", err)
	}
	retry, err := manager.AcquireDeployment(context.Background(), nil)
	if err != nil {
		t.Fatalf("AcquireDeployment after canceled waiter: %v", err)
	}
	if err := retry.Release(); err != nil {
		t.Fatalf("release retry deployment: %v", err)
	}
}

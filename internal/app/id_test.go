package app

import (
	"testing"
	"time"
)

func TestRestartOperationIDDistinguishesOperationsWithinOneSecond(t *testing.T) {
	// Given
	first := time.Date(2026, 7, 15, 10, 0, 0, 100, time.UTC)
	second := first.Add(time.Nanosecond)

	// When
	firstID := restartOperationID("my-app", "start", first)
	secondID := restartOperationID("my-app", "start", second)

	// Then
	if firstID == secondID {
		t.Fatalf("operation IDs collided within one second: %q", firstID)
	}
}

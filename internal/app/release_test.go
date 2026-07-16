package app

import (
	"context"
	"testing"
	"time"
)

func TestServiceReleaseHistoryIdentifiesCurrentAndLastSuccessful(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	service := NewService(store)
	old := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
	newer := old.Add(time.Hour)
	store.releases["rel_1"] = Release{ID: "rel_1", AppID: "app_1", Status: ReleaseStatusSucceeded, CreatedAt: old}
	store.releases["rel_2"] = Release{ID: "rel_2", AppID: "app_1", Status: ReleaseStatusFailed, CreatedAt: newer}

	history, err := service.ReleaseHistory(ctx, "app_1")
	if err != nil {
		t.Fatalf("ReleaseHistory: %v", err)
	}

	if len(history.Releases) != 2 {
		t.Fatalf("Releases = %#v", history.Releases)
	}
	if history.CurrentRelease == nil || history.CurrentRelease.ID != "rel_1" {
		t.Fatalf("CurrentRelease = %#v", history.CurrentRelease)
	}
	if history.LastSuccessfulRelease == nil || history.LastSuccessfulRelease.ID != "rel_1" {
		t.Fatalf("LastSuccessfulRelease = %#v", history.LastSuccessfulRelease)
	}
}

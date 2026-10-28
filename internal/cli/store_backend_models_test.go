package cli

import (
	"reflect"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestLatestAppRelease(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		releases []app.Release
		wantID   string
	}{
		{name: "empty"},
		{name: "zero time", releases: []app.Release{{ID: "one"}}, wantID: "one"},
		{name: "unsorted and tied", releases: []app.Release{
			{ID: "a", CreatedAt: now.Add(time.Hour)},
			{ID: "old", CreatedAt: now},
			{ID: "z", CreatedAt: now.Add(time.Hour)},
		}, wantID: "z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := append([]app.Release(nil), tt.releases...)
			got, ok := latestAppRelease(tt.releases)
			if ok != (tt.wantID != "") || got.ID != tt.wantID {
				t.Fatalf("latestAppRelease() = %q, %t; want %q", got.ID, ok, tt.wantID)
			}
			if !reflect.DeepEqual(tt.releases, before) {
				t.Fatal("latestAppRelease modified its input")
			}
		})
	}
}

func TestLatestAppDeployment(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		deployments []app.Deployment
		wantID      string
	}{
		{name: "empty"},
		{name: "zero time", deployments: []app.Deployment{{ID: "one"}}, wantID: "one"},
		{name: "unsorted and tied", deployments: []app.Deployment{
			{ID: "a", StartedAt: now.Add(time.Hour)},
			{ID: "old", StartedAt: now},
			{ID: "z", StartedAt: now.Add(time.Hour)},
		}, wantID: "z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := append([]app.Deployment(nil), tt.deployments...)
			got, ok := latestAppDeployment(tt.deployments)
			if ok != (tt.wantID != "") || got.ID != tt.wantID {
				t.Fatalf("latestAppDeployment() = %q, %t; want %q", got.ID, ok, tt.wantID)
			}
			if !reflect.DeepEqual(tt.deployments, before) {
				t.Fatal("latestAppDeployment modified its input")
			}
		})
	}
}

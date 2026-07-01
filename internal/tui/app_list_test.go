package tui

import "testing"

func TestAppListScreenRowsExposeOperationalFields(t *testing.T) {
	screen := NewAppListScreen(AppListView{
		Items: []AppListItem{
			{
				ID:                  "app_1",
				Name:                "my-app",
				Status:              "healthy",
				NodeID:              "local",
				LatestReleaseStatus: "succeeded",
				DomainCount:         2,
			},
		},
	})

	rows := screen.Rows()
	if len(rows) != 1 {
		t.Fatalf("Rows = %#v", rows)
	}
	row := rows[0]
	if row.Name != "my-app" {
		t.Fatalf("Name = %q", row.Name)
	}
	if row.Status != "healthy" {
		t.Fatalf("Status = %q", row.Status)
	}
	if row.NodeID != "local" {
		t.Fatalf("NodeID = %q", row.NodeID)
	}
	if row.LatestReleaseStatus != "succeeded" {
		t.Fatalf("LatestReleaseStatus = %q", row.LatestReleaseStatus)
	}
	if row.DomainCount != 2 {
		t.Fatalf("DomainCount = %d", row.DomainCount)
	}
}

func TestAppListScreenEmptyState(t *testing.T) {
	screen := NewAppListScreen(AppListView{})

	if !screen.Empty() {
		t.Fatal("Empty = false, want true")
	}
	if screen.EmptyMessage() != "No apps" {
		t.Fatalf("EmptyMessage = %q", screen.EmptyMessage())
	}
}

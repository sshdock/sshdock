package tui

import "testing"

func TestLogsScreenShowsAppServiceAndLines(t *testing.T) {
	screen := NewLogsScreen(NewLogsView("app_1", "web", "first\nsecond\n"))

	if screen.Title() != "app_1/web logs" {
		t.Fatalf("Title = %q", screen.Title())
	}
	if len(screen.Lines()) != 2 || screen.Lines()[0] != "first" || screen.Lines()[1] != "second" {
		t.Fatalf("Lines = %#v", screen.Lines())
	}
}

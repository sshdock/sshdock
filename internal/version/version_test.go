package version

import "testing"

func TestStringReturnsDevelopmentVersion(t *testing.T) {
	got := String()
	want := "dev"

	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

package version

import "testing"

func TestStringReturnsDevelopmentVersion(t *testing.T) {
	got := String()
	want := "dev"

	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringReturnsInjectedVersion(t *testing.T) {
	original := value
	t.Cleanup(func() {
		value = original
	})

	value = "v0.1.0-m8"

	if got := String(); got != "v0.1.0-m8" {
		t.Fatalf("String() = %q, want %q", got, "v0.1.0-m8")
	}
}

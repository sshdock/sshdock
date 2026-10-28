package compose

import (
	"slices"
	"testing"
)

func TestEffectiveModelWarningsAreSorted(t *testing.T) {
	model := composeModel{
		"services": map[string]any{
			"z": map[string]any{"privileged": true},
			"a": map[string]any{"network_mode": "host"},
		},
		"volumes": map[string]any{
			"z": map[string]any{"external": true, "name": "z-data"},
			"a": map[string]any{"name": "a-data"},
		},
	}
	want := []string{
		"service a uses host networking; " + trustedComposeWarning,
		"service z uses privileged mode; " + trustedComposeWarning,
		"uses explicit volume name a-data; " + trustedComposeWarning,
		"uses external volume z-data; " + trustedComposeWarning,
	}
	if got := effectiveModelWarnings(model, "project"); !slices.Equal(got, want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

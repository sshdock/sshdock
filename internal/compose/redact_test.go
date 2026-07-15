package compose

import (
	"strings"
	"testing"
)

func TestRedactValuesReplacesOverlappingSecretsLongestFirst(t *testing.T) {
	// Given
	values := map[string]string{"short": "token", "long": "token-long", "duplicate": "token-long", "empty": ""}

	// When
	redacted := RedactValues("short=token long=token-long", values)

	// Then
	if strings.Contains(redacted, "token") || redacted != "short=<redacted> long=<redacted>" {
		t.Fatalf("RedactValues = %q", redacted)
	}
}

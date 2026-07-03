package harness_test

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseWorkflowInjectsTagVersion(t *testing.T) {
	workflow, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("ReadFile release workflow: %v", err)
	}

	text := string(workflow)
	for _, want := range []string{
		"-ldflags",
		"github.com/iketiunn/rumbase/internal/version.value=${{ github.ref_name }}",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("release workflow missing %q:\n%s", want, text)
		}
	}
}

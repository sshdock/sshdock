package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedReleaseOnboardingIsVersionMatched(t *testing.T) {
	for _, path := range []string{"README.md", "docs/INSTALL.md"} {
		t.Run(path, func(t *testing.T) {
			contents, err := os.ReadFile(filepath.Join(repoRoot(t), path))
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			released, development := splitPublishedReleaseDocs(t, string(contents))

			// Keep release downloads, complete usage docs, and the SSH account on
			// the same published version. Update this contract when it is replaced.
			for _, want := range []string{
				"https://github.com/sshdock/sshdock/releases/tag/v0.3.1",
				"https://raw.githubusercontent.com/sshdock/sshdock/v0.3.1/scripts/bootstrap.sh",
				"sudo SSHDOCK_TAG=v0.3.1 bash bootstrap.sh",
				"https://github.com/sshdock/sshdock/blob/v0.3.1/README.md#quick-start",
				"https://github.com/sshdock/sshdock/blob/v0.3.1/docs/INSTALL.md",
				"ssh dashboard@sshdock.example.com",
				"it does not have the newer `sshdock` operator account or durable deployment logs",
			} {
				if !strings.Contains(released, want) {
					t.Errorf("published release guidance missing %q", want)
				}
			}
			for _, reject := range []string{
				"ssh sshdock@",
				"/main/scripts/bootstrap.sh",
				"SSHDOCK_TAG=development",
			} {
				if strings.Contains(released, reject) {
					t.Errorf("published release guidance contains development instruction %q", reject)
				}
			}

			for _, want := range []string{
				"(Unreleased)",
				"not v0.3.1",
				"No published release currently provides this full command set.",
				"#local-development-install-unreleased",
			} {
				if !strings.Contains(development, want) {
					t.Errorf("development guidance missing version boundary %q", want)
				}
			}
			for _, reject := range []string{"SSHDOCK_TAG=v0.3.1", "/v0.3.1/scripts/bootstrap.sh", "dashboard@"} {
				if strings.Contains(development, reject) {
					t.Errorf("development guidance contains published-release instruction %q", reject)
				}
			}
		})
	}
}

func TestDevelopmentInstallUsesMatchingLocalBinaries(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "INSTALL.md"))
	if err != nil {
		t.Fatalf("read installation guide: %v", err)
	}
	_, development := splitPublishedReleaseDocs(t, string(contents))
	for _, want := range []string{
		"## Local Development Install (Unreleased)",
		"make build\nsudo SSHDOCK_TAG=development SSHDOCK_BOOTSTRAP_SOURCE_BIN_DIR=\"$PWD/bin\"",
		"bash scripts/bootstrap.sh",
		"It does not verify a published-release installation.",
	} {
		if !strings.Contains(development, want) {
			t.Errorf("local development installation missing %q", want)
		}
	}
}

// Only this explicitly versioned, bounded section may describe the old contract.
// Text before it and every following section remain subject to current guards.
func splitPublishedReleaseDocs(t *testing.T, text string) (string, string) {
	t.Helper()
	const heading = "## Published Release: v0.3.1\n"
	before, after, found := strings.Cut(text, heading)
	if !found {
		t.Fatalf("missing explicit published-release heading %q", heading)
	}
	end := strings.Index(after, "\n## ")
	if end < 0 {
		t.Fatal("published-release guidance must end before a separate development section")
	}
	return after[:end], before + after[end:]
}

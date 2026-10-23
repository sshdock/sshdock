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
			released, current := splitPublishedReleaseDocs(t, string(contents))
			for _, want := range []string{
				"https://github.com/sshdock/sshdock/releases/tag/v1.1.0",
				"https://raw.githubusercontent.com/sshdock/sshdock/v1.1.0/scripts/bootstrap.sh",
				"sudo SSHDOCK_TAG=v1.1.0 bash bootstrap.sh",
				"https://github.com/sshdock/sshdock/blob/v1.1.0/README.md#quick-start",
				"https://github.com/sshdock/sshdock/blob/v1.1.0/docs/INSTALL.md",
				"ssh sshdock@sshdock.example.com",
				"durable deployment logs",
			} {
				if !strings.Contains(released, want) {
					t.Errorf("published release guidance missing %q", want)
				}
			}
			for _, reject := range []string{
				"dashboard@", "/main/scripts/bootstrap.sh", "SSHDOCK_TAG=development", "SSHDOCK_TAG=v0.3.1",
			} {
				if strings.Contains(released, reject) {
					t.Errorf("published release guidance contains mismatched instruction %q", reject)
				}
			}
			for _, reject := range []string{
				"No published release currently provides this full command set.", "not v0.3.1",
				"dashboard@", "SSHDOCK_TAG=v0.3.1", "/v0.3.1/scripts/bootstrap.sh",
			} {
				if strings.Contains(current, reject) {
					t.Errorf("current guidance contains obsolete release boundary %q", reject)
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
	_, current := splitPublishedReleaseDocs(t, string(contents))
	for _, want := range []string{
		"## Local Development Install (Unreleased)",
		"make build\nsudo SSHDOCK_TAG=development SSHDOCK_BOOTSTRAP_SOURCE_BIN_DIR=\"$PWD/bin\"",
		"bash scripts/bootstrap.sh",
		"It does not verify a published-release installation.",
	} {
		if !strings.Contains(current, want) {
			t.Errorf("local development installation missing %q", want)
		}
	}
}

func TestPublicExampleDownloadsUseMatchingRelease(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(repoRoot(t), "examples", "*", "*", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(contents)
		for _, reject := range []string{
			"https://github.com/sshdock/sshdock/archive/refs/heads/main",
			"https://raw.githubusercontent.com/sshdock/sshdock/main/",
		} {
			if strings.Contains(text, reject) {
				t.Errorf("%s mixes release guidance with mutable source %q", path, reject)
			}
		}
		if strings.Contains(text, "https://github.com/sshdock/sshdock/archive/") {
			for _, want := range []string{
				"https://github.com/sshdock/sshdock/archive/refs/tags/v1.1.0.tar.gz",
				"--strip-components=4 sshdock-1.1.0/examples/",
			} {
				if !strings.Contains(text, want) {
					t.Errorf("%s missing version-matched download %q", path, want)
				}
			}
		}
	}
}

// Keep the install command and linked complete usage docs in one versioned section.
func splitPublishedReleaseDocs(t *testing.T, text string) (string, string) {
	t.Helper()
	const heading = "## Published Release: v1.1.0\n"
	before, after, found := strings.Cut(text, heading)
	if !found {
		t.Fatalf("missing explicit published-release heading %q", heading)
	}
	end := strings.Index(after, "\n## ")
	if end < 0 {
		t.Fatal("published-release guidance must end before a separate usage section")
	}
	return after[:end], before + after[end:]
}

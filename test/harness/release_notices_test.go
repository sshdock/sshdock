package harness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseNotices(t *testing.T) {
	for _, test := range []struct {
		name, failure string
	}{
		{"compiled dependency notices", ""},
		{"missing module license", "license"},
		{"go list failure", "list"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range map[string]string{
				"go-root/LICENSE":                     "Go license text\n",
				"go-root/PATENTS":                     "Go patent grant\n",
				"go-root/src/vendor/NOTICE":           "Bundled toolchain notice\n",
				"module with spaces/licence":          "Dependency license text\n",
				"module with spaces/NOTICE":           "Dependency attribution\n",
				"module with spaces/vendor/legal.txt": "Bundled dependency terms\n",
				"main/LICENSE":                        "Main license must not be repeated\n",
				"go": `#!/bin/sh
case "$1 $2" in
  'env GOROOT') printf '%s/go-root\n' "$FIXTURE" ;;
  'env GOVERSION') printf 'go1.26.10\n' ;;
  'list -deps')
    printf 'github.com/example/dependency\tv1.2.3\t%s/module with spaces\tfalse\n' "$FIXTURE"
    printf 'github.com/example/dependency\tv1.2.3\t%s/module with spaces\tfalse\n' "$FIXTURE"
    printf 'github.com/sshdock/sshdock\t(devel)\t%s/main\ttrue\n' "$FIXTURE"
    if [ "$FAILURE" = list ]; then exit 17; fi ;;
  *) exit 91 ;;
esac
`,
			} {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if test.failure == "license" {
				if err := os.Remove(filepath.Join(root, "module with spaces/licence")); err != nil {
					t.Fatal(err)
				}
			}
			outputPath := filepath.Join(root, "THIRD_PARTY_NOTICES")
			if err := os.WriteFile(outputPath, []byte("previous notices"), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("bash", "../../scripts/release-notices.sh", outputPath)
			command.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"), "FIXTURE="+root, "FAILURE="+test.failure)
			output, err := command.CombinedOutput()
			if (err != nil) != (test.failure != "") {
				t.Fatalf("result = %v, output:\n%s", err, output)
			}
			content, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if test.failure != "" {
				if string(content) != "previous notices" {
					t.Fatal("failed collector replaced previous notices")
				}
				return
			}
			for _, want := range []string{"Go license text", "Go patent grant", "Bundled toolchain notice", "Dependency license text", "Dependency attribution", "Bundled dependency terms"} {
				if strings.Count(string(content), want) != 1 {
					t.Errorf("expected exactly one %q in notices", want)
				}
			}
			for _, unwanted := range []string{root, "Main license must not be repeated"} {
				if strings.Contains(string(content), unwanted) {
					t.Errorf("notices contain %q", unwanted)
				}
			}
		})
	}
}

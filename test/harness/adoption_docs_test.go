package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdoptionDocsExistAndKeepV0Boundaries(t *testing.T) {
	root := repoRoot(t)
	docs := []struct {
		path string
		want []string
	}{
		{
			path: filepath.Join(root, "docs", "COMPARE_DOKKU.md"),
			want: []string{
				"SSHDock vs Dokku",
				"Compose-first",
				"not a drop-in Dokku replacement",
				"MIGRATE_FROM_DOKKU.md",
			},
		},
		{
			path: filepath.Join(root, "docs", "COMPARE_DOKPLOY.md"),
			want: []string{
				"SSHDock vs Dokploy",
				"not a claim that SSHDock replaces Dokploy",
				"No web dashboard",
				"MIGRATE_FROM_DOKPLOY.md",
			},
		},
		{
			path: filepath.Join(root, "docs", "MIGRATE_FROM_DOKKU.md"),
			want: []string{
				"does not run Procfiles, buildpacks, Dokku plugins",
				"COMPOSE_SUPPORT.md",
				"config set my-app DATABASE_URL",
				"backup create",
			},
		},
		{
			path: filepath.Join(root, "docs", "MIGRATE_FROM_DOKPLOY.md"),
			want: []string{
				"does not provide a web dashboard",
				"Docker Stack",
				"config import my-app",
				"domains check my-app",
			},
		},
		{
			path: filepath.Join(root, "docs", "TROUBLESHOOTING.md"),
			want: []string{
				"sudo sshdock diagnostics",
				"stage=...",
				"domains check",
				"Do not append a remote `dashboard` command.",
			},
		},
	}

	for _, doc := range docs {
		t.Run(filepath.Base(doc.path), func(t *testing.T) {
			contents, err := os.ReadFile(doc.path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", doc.path, err)
			}
			text := string(contents)
			for _, want := range doc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("%s missing %q", doc.path, want)
				}
			}
		})
	}
}

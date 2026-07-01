//go:build e2e

package e2e

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iketiunn/rumbase/internal/app"
	"github.com/iketiunn/rumbase/internal/config"
	"github.com/iketiunn/rumbase/internal/store"
)

func TestGitHookEndToEnd(t *testing.T) {
	requireGit(t)

	ctx := context.Background()
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}

	rhumbasePath := filepath.Join(binDir, "rhumbase")
	rhumbasedPath := filepath.Join(binDir, "rhumbased")
	runCommand(t, root, nil, "go", "build", "-o", rhumbasePath, "./cmd/rhumbase")
	runCommand(t, root, nil, "go", "build", "-o", rhumbasedPath, "./cmd/rhumbased")

	dataDir := filepath.Join(tmp, "data")
	t.Setenv("RHUMBASE_DATA_DIR", dataDir)
	t.Setenv("RHUMBASE_COMPOSE_RUNNER", "fake")
	cfg := config.LoadFromEnv()
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll data dir: %v", err)
	}

	env := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_DATA_DIR="+dataDir,
		"RHUMBASE_COMPOSE_RUNNER=fake",
	)
	runCommand(t, root, env, rhumbasePath, "apps", "create", "my-app")

	sqlite, err := store.OpenSQLite(ctx, cfg.SQLiteDBPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	createdApp, err := sqlite.GetApp(ctx, "my-app")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatalf("Close app store: %v", err)
	}
	if createdApp.RepoPath != cfg.AppRepoPath("my-app") {
		t.Fatalf("app repo path = %q, want %q", createdApp.RepoPath, cfg.AppRepoPath("my-app"))
	}

	repoPath := cfg.AppRepoPath("my-app")
	if info, err := os.Stat(repoPath); err != nil {
		t.Fatalf("stat repo: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("repo path is not a directory: %s", repoPath)
	}

	hookPath := filepath.Join(repoPath, "hooks", "post-receive")
	if info, err := os.Stat(hookPath); err != nil {
		t.Fatalf("stat hook: %v", err)
	} else if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("hook is not executable: %v", info.Mode().Perm())
	}

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source: %v", err)
	}
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "Rhumbase Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte("services:\n  web:\n    image: example/web:latest\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "initial compose app")
	commitSHA := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	runGit(t, sourceDir, nil, "remote", "add", "prod", repoPath)

	runGit(t, sourceDir, env, "push", "prod", "main")

	releases, err := listReleases(cfg.SQLiteDBPath, "my-app")
	if err != nil {
		t.Fatalf("listReleases: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %#v", releases)
	}
	if releases[0].CommitSHA != commitSHA {
		t.Fatalf("release commit = %q, want %q", releases[0].CommitSHA, commitSHA)
	}

	status, err := deploymentStatus(cfg.SQLiteDBPath, "dep_"+shortSHA(commitSHA))
	if err != nil {
		t.Fatalf("deploymentStatus: %v", err)
	}
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
}

type releaseRow struct {
	ID        string
	CommitSHA string
}

func listReleases(dbPath string, appID string) ([]releaseRow, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`select id, commit_sha from releases where app_id = ? order by created_at`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var releases []releaseRow
	for rows.Next() {
		var row releaseRow
		if err := rows.Scan(&row.ID, &row.CommitSHA); err != nil {
			return nil, err
		}
		releases = append(releases, row)
	}
	return releases, rows.Err()
}

func deploymentStatus(dbPath string, deploymentID string) (string, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	var status string
	err = db.QueryRow(`select status from deployments where id = ?`, deploymentID).Scan(&status)
	return status, err
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required for e2e test: %v", err)
	}
}

func runGit(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	runCommand(t, dir, env, "git", args...)
}

func runGitOutput(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	return runCommand(t, dir, env, "git", args...)
}

func runCommand(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func shortSHA(commitSHA string) string {
	if len(commitSHA) <= 12 {
		return commitSHA
	}
	return commitSHA[:12]
}

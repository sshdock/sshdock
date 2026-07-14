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

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/store"
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

	sshdockPath := filepath.Join(binDir, "sshdock")
	sshdockdPath := filepath.Join(binDir, "sshdockd")
	runCommand(t, root, nil, "go", "build", "-o", sshdockPath, "./cmd/sshdock")
	runCommand(t, root, nil, "go", "build", "-o", sshdockdPath, "./cmd/sshdockd")

	dataDir := filepath.Join(tmp, "data")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	cfg := config.LoadFromEnv()
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll data dir: %v", err)
	}

	env := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_COMPOSE_RUNNER=fake",
	)
	runCommand(t, root, env, sshdockPath, "apps", "create", "my-app")

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
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yaml"), []byte("services:\n  web:\n    image: example/web:latest\n    command: [\"./serve\"]\n    labels: {com.example.role: web}\n    networks: [frontend]\n    configs: [app-config]\n    secrets: [app-secret]\n    deploy: {resources: {limits: {memory: 256M}}}\nnetworks: {frontend: {}}\nconfigs: {app-config: {file: ./app.conf}}\nsecrets: {app-secret: {file: ./app.secret}}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	runGit(t, sourceDir, nil, "add", "compose.yaml")
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

	status, err := deploymentStatusForCommit(cfg.SQLiteDBPath, "my-app", commitSHA, app.DeploymentTriggerPush)
	if err != nil {
		t.Fatalf("deploymentStatus: %v", err)
	}
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
}

func TestGitReceivePushToCreateEndToEnd(t *testing.T) {
	requireGit(t)

	ctx := context.Background()
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}

	sshdockdPath := filepath.Join(binDir, "sshdockd")
	runCommand(t, root, nil, "go", "build", "-o", sshdockdPath, "./cmd/sshdockd")

	fakeSSHPath := filepath.Join(binDir, "fake-ssh")
	if err := os.WriteFile(fakeSSHPath, []byte(`#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
	case "$1" in
		-o|-p|-l|-i|-F|-S|-J|-b|-c|-m)
			shift 2
			;;
		-*)
			shift
			;;
		*)
			break
			;;
	esac
done
if [ "$#" -lt 2 ]; then
	echo "missing SSH original command" >&2
	exit 2
fi
shift
SSH_ORIGINAL_COMMAND="$*" exec sshdockd git-receive
`), 0o755); err != nil {
		t.Fatalf("WriteFile fake ssh: %v", err)
	}

	appName := "push-app"
	dataDir := filepath.Join(tmp, "data")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	cfg := config.LoadFromEnv()
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll data dir: %v", err)
	}

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source: %v", err)
	}
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte("services:\n  web:\n    image: example/web:latest\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "initial push-to-create compose app")
	commitSHA := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	runGit(t, sourceDir, nil, "remote", "add", "sshdock", "git@server:"+appName+".git")

	env := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_SSH="+fakeSSHPath,
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_COMPOSE_RUNNER=fake",
	)
	runGit(t, sourceDir, env, "push", "sshdock", "main")

	sqlite, err := store.OpenSQLite(ctx, cfg.SQLiteDBPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	createdApp, err := sqlite.GetApp(ctx, appName)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatalf("Close app store: %v", err)
	}
	if createdApp.RepoPath != cfg.AppRepoPath(appName) {
		t.Fatalf("app repo path = %q, want %q", createdApp.RepoPath, cfg.AppRepoPath(appName))
	}
	if info, err := os.Stat(cfg.AppRepoPath(appName)); err != nil {
		t.Fatalf("stat repo: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("repo path is not a directory: %s", cfg.AppRepoPath(appName))
	}

	releases, err := listReleases(cfg.SQLiteDBPath, appName)
	if err != nil {
		t.Fatalf("listReleases: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("releases = %#v", releases)
	}
	if releases[0].CommitSHA != commitSHA {
		t.Fatalf("release commit = %q, want %q", releases[0].CommitSHA, commitSHA)
	}

	status, err := deploymentStatusForCommit(cfg.SQLiteDBPath, appName, commitSHA, app.DeploymentTriggerPush)
	if err != nil {
		t.Fatalf("deploymentStatus: %v", err)
	}
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
}

func TestGitHookDockerComposeEndToEnd(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run the Docker Compose e2e test")
	}
	requireGit(t)
	requireDocker(t)

	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}

	sshdockPath := filepath.Join(binDir, "sshdock")
	sshdockdPath := filepath.Join(binDir, "sshdockd")
	runCommand(t, root, nil, "go", "build", "-o", sshdockPath, "./cmd/sshdock")
	runCommand(t, root, nil, "go", "build", "-o", sshdockdPath, "./cmd/sshdockd")

	appName := "docker-app"
	projectName := compose.ProjectName(appName)
	dataDir := filepath.Join(tmp, "data")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "docker")
	cfg := config.LoadFromEnv()
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll data dir: %v", err)
	}
	t.Cleanup(func() {
		composePath := filepath.Join(cfg.AppWorktreePath(appName), "compose.yml")
		if _, err := os.Stat(composePath); err == nil {
			_ = runCommandNoFail(filepath.Dir(composePath), nil, "docker", "compose", "-f", composePath, "-p", projectName, "down", "-v", "--remove-orphans")
		}
	})

	env := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_COMPOSE_RUNNER=docker",
	)
	runCommand(t, root, env, sshdockPath, "apps", "create", appName)

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source: %v", err)
	}
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte("services:\n  web:\n    image: nginx:alpine\n"), 0o644); err != nil {
		t.Fatalf("WriteFile compose: %v", err)
	}
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "initial docker compose app")
	commitSHA := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	runGit(t, sourceDir, nil, "remote", "add", "prod", cfg.AppRepoPath(appName))

	runGit(t, sourceDir, env, "push", "prod", "main")

	status, err := deploymentStatusForCommit(cfg.SQLiteDBPath, appName, commitSHA, app.DeploymentTriggerPush)
	if err != nil {
		t.Fatalf("deploymentStatus: %v", err)
	}
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}

	composePath := filepath.Join(cfg.AppWorktreePath(appName), "compose.yml")
	output := runCommand(t, filepath.Dir(composePath), nil, "docker", "compose", "-f", composePath, "-p", projectName, "ps", "--format", "json")
	if !strings.Contains(output, `"Service":"web"`) && !strings.Contains(output, `"Name":"web"`) {
		t.Fatalf("docker compose ps output missing web service:\n%s", output)
	}
	if !strings.Contains(output, `"State":"running"`) {
		t.Fatalf("docker compose ps output missing running state:\n%s", output)
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

func deploymentStatusForCommit(dbPath string, appID string, commitSHA string, trigger app.DeploymentTrigger) (string, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	var status string
	err = db.QueryRow(`
		select status from deployments
		where app_id = ? and commit_sha = ? and trigger = ?
		order by started_at desc, id desc limit 1`, appID, commitSHA, string(trigger)).Scan(&status)
	return status, err
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required for e2e test: %v", err)
	}
}

func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker is required for Docker e2e test: %v", err)
	}
	runCommand(t, "", nil, "docker", "version")
	runCommand(t, "", nil, "docker", "compose", "version")
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

func runCommandNoFail(dir string, env []string, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	output, _ := cmd.CombinedOutput()
	return string(output)
}

func shortSHA(commitSHA string) string {
	if len(commitSHA) <= 12 {
		return commitSHA
	}
	return commitSHA[:12]
}

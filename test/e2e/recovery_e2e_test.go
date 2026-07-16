//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/config"
)

func TestRecoverySelectsKnownGoodRevisionThroughRemoteMainEndToEnd(t *testing.T) {
	requireGit(t)

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

	appName := "recovery-app"
	dataDir := filepath.Join(tmp, "data")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	t.Setenv("SSHDOCK_COMPOSE_RUNNER", "fake")
	cfg := config.LoadFromEnv()

	baseEnv := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_COMPOSE_RUNNER=fake",
	)
	runCommand(t, root, baseEnv, sshdockPath, "apps", "create", appName)

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source: %v", err)
	}
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	writeRecoveryCompose(t, sourceDir, "example/web:good")
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "good compose app")
	goodCommit := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	goodReleaseID := app.ReleaseID(appName, goodCommit)
	runGit(t, sourceDir, nil, "remote", "add", "prod", cfg.AppRepoPath(appName))

	runGit(t, sourceDir, baseEnv, "push", "prod", "main")

	dbPath := cfg.SQLiteDBPath
	assertAppStatus(t, dbPath, appName, app.AppStatusHealthy)
	assertReleaseStatus(t, dbPath, goodReleaseID, app.ReleaseStatusSucceeded)
	if status, err := deploymentStatusForCommit(dbPath, appName, goodCommit, app.DeploymentTriggerPush); err != nil {
		t.Fatalf("deploymentStatus good: %v", err)
	} else if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("good deployment status = %q", status)
	}

	writeRecoveryCompose(t, sourceDir, "example/web:bad")
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "bad compose app")
	badCommit := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	badReleaseID := app.ReleaseID(appName, badCommit)
	failingEnv := append(baseEnv, "SSHDOCK_FAKE_COMPOSE_DEPLOY_ERROR=compose failed")
	runGitAllowError(t, sourceDir, failingEnv, "push", "prod", "main")

	assertAppStatus(t, dbPath, appName, app.AppStatusFailed)
	assertReleaseStatus(t, dbPath, badReleaseID, app.ReleaseStatusFailed)
	if status, err := deploymentStatusForCommit(dbPath, appName, badCommit, app.DeploymentTriggerPush); err != nil {
		t.Fatalf("deploymentStatus bad: %v", err)
	} else if status != string(app.DeploymentStatusFailed) {
		t.Fatalf("bad deployment status = %q", status)
	}

	runGit(t, sourceDir, baseEnv, "push", "--force", "prod", goodCommit+":main")

	assertAppStatus(t, dbPath, appName, app.AppStatusHealthy)
	assertReleaseStatus(t, dbPath, goodReleaseID, app.ReleaseStatusSucceeded)
	assertReleaseStatus(t, dbPath, badReleaseID, app.ReleaseStatusFailed)
	if status, err := deploymentStatusForCommit(dbPath, appName, goodCommit, app.DeploymentTriggerPush); err != nil {
		t.Fatalf("deploymentStatus recovered good commit: %v", err)
	} else if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("recovered good deployment status = %q", status)
	}
	if rollbackAttempts := queryString(t, dbPath, `select count(*) from deployments where app_id = ? and trigger = 'rollback'`, appName); rollbackAttempts != "0" {
		t.Fatalf("SSHDock rollback attempts = %q, want 0", rollbackAttempts)
	}
}

func writeRecoveryCompose(t *testing.T, sourceDir string, image string) {
	t.Helper()

	content := "services:\n  web:\n    image: " + image + "\n"
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile compose.yml: %v", err)
	}
}

func runGitAllowError(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	output, _ := cmd.CombinedOutput()
	return string(output)
}

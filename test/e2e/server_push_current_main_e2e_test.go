//go:build e2e

package e2e

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	_ "modernc.org/sqlite"
)

func TestServerPushCurrentMainSemanticsEndToEnd(t *testing.T) {
	// Given
	paths := setupBootstrappedServerPush(t, "fake")
	appName := "current-main-app"
	initialCommit, initialOutput := pushComposeAppThroughSSHWithOutput(t, paths, appName, map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:initial\n",
	})
	assertPushOutputContains(t, initialOutput,
		"git: remote main updated to "+initialCommit,
		"deploy: current main "+initialCommit+" succeeded",
	)
	sourceDir := filepath.Join(paths.tmp, "source-"+appName)
	pushEnv := currentMainPushEnv(t, paths)
	repoPath := filepath.Join(paths.dataDir, "apps", appName, "repo.git")
	runGit(t, sourceDir, nil, "checkout", "-b", "feature")
	writeCurrentMainCompose(t, sourceDir, "example/web:feature")
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "Feature deploy")
	featureCommit := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))

	// When: a non-main destination is rejected before its ref changes.
	nonMainOutput, nonMainErr := runCurrentMainGitAttempt(sourceDir, pushEnv, "push", "sshdock", "feature")

	// Then
	if nonMainErr == nil || !strings.Contains(nonMainOutput, "push to remote main") {
		t.Fatalf("non-main push error = %v, output:\n%s", nonMainErr, nonMainOutput)
	}
	assertRemoteMain(t, repoPath, initialCommit)

	// When: any local branch may explicitly target remote main.
	branchOutput, branchErr := runCurrentMainGitAttempt(sourceDir, pushEnv, "push", "sshdock", "feature:main")

	// Then
	if branchErr != nil {
		t.Fatalf("branch-to-main push: %v\n%s", branchErr, branchOutput)
	}
	assertRemoteMain(t, repoPath, featureCommit)

	// When: an explicit redeploy retries the exact current-main commit.
	dbPath := filepath.Join(paths.dataDir, "sshdock.db")
	beforeRedeploy := currentMainAttemptCount(t, dbPath, appName, featureCommit)
	cliEnv := append(os.Environ(),
		"PATH="+paths.installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+paths.dataDir,
		"SSHDOCK_COMPOSE_RUNNER=fake",
	)
	redeployOutput := runCommand(t, filepath.Join("..", ".."), cliEnv, filepath.Join(paths.installBinDir, "sshdock"), "apps", "redeploy", appName)

	// Then
	if !strings.Contains(redeployOutput, "redeployed current main for "+appName) {
		t.Fatalf("redeploy output = %q", redeployOutput)
	}
	if got := currentMainAttemptCount(t, dbPath, appName, featureCommit); got != beforeRedeploy+1 {
		t.Fatalf("current-main attempts = %d, want %d", got, beforeRedeploy+1)
	}

	// When: a peeled annotated tag may explicitly select remote main.
	runGit(t, sourceDir, nil, "tag", "-a", "issue5-initial", initialCommit, "-m", "Known good")
	tagOutput, tagErr := runCurrentMainGitAttempt(sourceDir, pushEnv, "push", "--force", "sshdock", "issue5-initial^{}:refs/heads/main")
	if tagErr != nil {
		t.Fatalf("annotated-tag-to-main push: %v\n%s", tagErr, tagOutput)
	}
	assertPushOutputContains(t, tagOutput,
		"git: remote main updated to "+initialCommit,
		"deploy: current main "+initialCommit+" succeeded",
	)
	assertRemoteMain(t, repoPath, initialCommit)
	branchOutput, branchErr = runCurrentMainGitAttempt(sourceDir, pushEnv, "push", "sshdock", "feature:main")
	if branchErr != nil {
		t.Fatalf("restore feature to main: %v\n%s", branchErr, branchOutput)
	}

	// When: a failed deployment still leaves the accepted commit at remote main.
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte("services: [\n"), 0o644); err != nil {
		t.Fatalf("write invalid compose: %v", err)
	}
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "Break deployment")
	failedCommit := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	failedOutput, failedPushErr := runCurrentMainGitAttempt(sourceDir, pushEnv, "push", "sshdock", "feature:main")

	// Then
	if failedPushErr != nil {
		t.Fatalf("post-receive deployment failure rejected Git update: %v\n%s", failedPushErr, failedOutput)
	}
	assertPushOutputContains(t, failedOutput,
		"git: remote main updated to "+failedCommit,
		"deploy: failed: current main "+failedCommit,
	)
	assertRemoteMain(t, repoPath, failedCommit)
	failedHealth := runCommand(t, filepath.Join("..", ".."), cliEnv, filepath.Join(paths.installBinDir, "sshdock"), "apps", "health", appName)
	for _, want := range []string{
		"health: fail",
		"current main: " + failedCommit,
		"latest deploy: dep_",
		"failed commit=" + failedCommit + " trigger=push",
		"services: -",
		"last failure: dep_",
	} {
		if !strings.Contains(failedHealth, want) {
			t.Fatalf("failed-push health output missing %q:\n%s", want, failedHealth)
		}
	}

	// When: force-pushing an older commit to main performs Git-based rollback.
	beforeRollback := currentMainAttemptCount(t, dbPath, appName, initialCommit)
	rollbackOutput, rollbackErr := runCurrentMainGitAttempt(sourceDir, pushEnv, "push", "--force", "sshdock", initialCommit+":main")

	// Then
	if rollbackErr != nil {
		t.Fatalf("Git rollback push: %v\n%s", rollbackErr, rollbackOutput)
	}
	assertPushOutputContains(t, rollbackOutput, "deploy: current main "+initialCommit+" succeeded")
	assertRemoteMain(t, repoPath, initialCommit)
	if got := currentMainAttemptCount(t, dbPath, appName, initialCommit); got != beforeRollback+1 {
		t.Fatalf("rollback attempts = %d, want %d", got, beforeRollback+1)
	}
	assertAppStatus(t, dbPath, appName, app.AppStatusHealthy)
}

func currentMainPushEnv(t *testing.T, paths serverPushPaths) []string {
	t.Helper()
	sshPath := requireCommandOrSkip(t, "ssh")
	sshCommand := fmt.Sprintf("%s -p %d -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", sshPath, paths.sshPort, paths.clientKeyPath)
	return append(os.Environ(),
		"PATH="+paths.installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_SSH_COMMAND="+sshCommand,
		"SSHDOCK_DATA_DIR="+paths.dataDir,
	)
}

func runCurrentMainGitAttempt(dir string, env []string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = env
	output, err := command.CombinedOutput()
	return string(output), err
}

func writeCurrentMainCompose(t *testing.T, sourceDir string, image string) {
	t.Helper()
	content := "services:\n  web:\n    image: " + image + "\n"
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
}

func assertPushOutputContains(t *testing.T, output string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(output, value) {
			t.Fatalf("push output missing %q:\n%s", value, output)
		}
	}
}

func assertRemoteMain(t *testing.T, repoPath string, want string) {
	t.Helper()
	got := strings.TrimSpace(runGitOutput(t, "", nil, "--git-dir", repoPath, "rev-parse", "refs/heads/main"))
	if got != want {
		t.Fatalf("remote main = %q, want %q", got, want)
	}
}

func currentMainAttemptCount(t *testing.T, dbPath string, appName string, commitSHA string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow(`select count(*) from deployments where app_id = ? and commit_sha = ?`, appName, commitSHA).Scan(&count); err != nil {
		t.Fatalf("query deployment attempts: %v", err)
	}
	return count
}

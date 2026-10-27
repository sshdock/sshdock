//go:build e2e

package e2e

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/config"
	"github.com/sshdock/sshdock/internal/gitrecv"
)

func TestRecoverySelectsKnownGoodRevisionThroughRemoteMainEndToEnd(t *testing.T) {
	requireGit(t)
	operator, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSHDOCK_OPERATOR_USER", operator.Username)

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
	writeDockerFixture(t, binDir)

	appName := "recovery-app"
	dataDir := filepath.Join(tmp, "data")
	t.Setenv("SSHDOCK_DATA_DIR", dataDir)
	cfg := config.LoadFromEnv()

	baseEnv := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+dataDir,
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

	startDeploymentDaemon(t, sshdockdPath, baseEnv)
	goodAttempt := recoveryAttemptID(t, runGitOutput(t, sourceDir, baseEnv, "push", "prod", "main"))
	waitForRecoveryAttempt(t, cfg.SQLiteDBPath, appName, goodAttempt, goodCommit, app.DeploymentStatusSucceeded)

	dbPath := cfg.SQLiteDBPath
	assertAppStatus(t, dbPath, appName, app.AppStatusHealthy)
	assertReleaseStatus(t, dbPath, goodReleaseID, app.ReleaseStatusSucceeded)
	if status, err := deploymentStatusForCommit(dbPath, appName, goodCommit, app.DeploymentTriggerPush); err != nil {
		t.Fatalf("deploymentStatus good: %v", err)
	} else if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("good deployment status = %q", status)
	}

	writeRecoveryCompose(t, sourceDir, "${MISSING_RECOVERY_IMAGE:?set an image}")
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "bad compose app")
	badCommit := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	badReleaseID := app.ReleaseID(appName, badCommit)
	badAttempt := recoveryAttemptID(t, runGitOutput(t, sourceDir, baseEnv, "push", "prod", "main"))
	waitForRecoveryAttempt(t, dbPath, appName, badAttempt, badCommit, app.DeploymentStatusFailed)

	assertAppStatus(t, dbPath, appName, app.AppStatusFailed)
	assertReleaseStatus(t, dbPath, badReleaseID, app.ReleaseStatusFailed)
	if status, err := deploymentStatusForCommit(dbPath, appName, badCommit, app.DeploymentTriggerPush); err != nil {
		t.Fatalf("deploymentStatus bad: %v", err)
	} else if status != string(app.DeploymentStatusFailed) {
		t.Fatalf("bad deployment status = %q", status)
	}

	recoveredAttempt := recoveryAttemptID(t, runGitOutput(t, sourceDir, baseEnv, "push", "--force-with-lease=refs/heads/main:"+badCommit, "prod", goodCommit+":refs/heads/main"))
	if recoveredAttempt == goodAttempt || recoveredAttempt == badAttempt {
		t.Fatal("recovery reused an existing deployment attempt")
	}
	waitForRecoveryAttempt(t, dbPath, appName, recoveredAttempt, goodCommit, app.DeploymentStatusSucceeded)

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

func TestRecoveryDockerReplacementFailureAndFailedRollbackEndToEnd(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run Docker recovery acceptance")
	}
	requireDocker(t)
	isolationDir := t.TempDir()
	t.Setenv("SSHDOCK_CADDY_CONFIG_PATH", filepath.Join(isolationDir, "routes.caddyfile"))
	t.Setenv("SSHDOCK_CADDY_MAIN_CONFIG_PATH", filepath.Join(isolationDir, "main.caddyfile"))
	t.Setenv("SSHDOCK_CADDY_ADMIN_ADDRESS", "127.0.0.1:1")
	t.Setenv("SSHDOCK_BOOTSTRAP_FAKE_LOG", filepath.Join(isolationDir, "fake-commands.log"))
	for _, name := range []string{"SSHDOCK_DATA_DIR", "SSHDOCK_SQLITE_DB_PATH", "SSHDOCK_APPS_DIR", "SSHDOCK_LOCKS_DIR", "SSHDOCK_CONFIG_KEY_PATH", "SSHDOCK_GIT_HOME_DIR"} {
		t.Setenv(name, "")
	}
	t.Setenv("RECOVERY_READY", "ready")
	paths := setupBootstrappedServerPush(t, "docker")
	appName := fmt.Sprintf("recovery-docker-%d", time.Now().UnixNano())
	port := freeLocalPort(t)
	dbPath := filepath.Join(paths.dataDir, "sshdock.db")
	repoPath := filepath.Join(paths.dataDir, "apps", appName, "repo.git")
	worktree := filepath.Join(paths.dataDir, "apps", appName, "worktree")
	project, projectErr := compose.ProjectName(appName)
	if projectErr != nil {
		t.Fatal(projectErr)
	}
	cliPath := filepath.Join(paths.installBinDir, "sshdock")
	cliEnv := append(os.Environ(),
		"PATH="+filepath.Join(paths.tmp, "fake-bin")+string(os.PathListSeparator)+paths.installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+paths.dataDir,
	)
	t.Cleanup(func() {
		runCommandNoFail("", cliEnv, "docker", "compose", "-f", filepath.Join(worktree, "compose.yml"), "-p", project, "down", "--remove-orphans")
		runCommandNoFail("", nil, "docker", "volume", "rm", project+"_data")
	})

	goodCommit, goodOutput := pushComposeAppThroughSSHWithOutput(t, paths, appName, map[string]string{
		"compose.yml": recoveryDockerCompose(port, "A", true),
	})
	goodAttempt := recoveryAttemptID(t, goodOutput)
	waitForRecoveryAttempt(t, dbPath, appName, goodAttempt, goodCommit, app.DeploymentStatusSucceeded)
	goodContainer := recoveryContainer(t, project)
	seed := "written-after-first-success"
	runCommand(t, "", nil, "docker", "exec", goodContainer, "sh", "-c", `printf '%s' "$1" > /data/sentinel`, "_", seed)
	assertRecoveryRuntime(t, goodContainer, port, "A", "healthy", seed)

	// The failed update passes native validation and actually replaces the healthy container.
	sourceDir := filepath.Join(paths.tmp, "source-"+appName)
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte(recoveryDockerCompose(port, "B", false)), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, sourceDir, nil, "add", "compose.yml")
	runGit(t, sourceDir, nil, "commit", "-m", "Replace with unhealthy B")
	badCommit := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	pushEnv := currentMainPushEnv(t, paths)
	badAttempt := recoveryAttemptID(t, runGitOutput(t, sourceDir, pushEnv, "push", "sshdock", "main"))
	waitForRecoveryAttempt(t, dbPath, appName, badAttempt, badCommit, app.DeploymentStatusFailed)
	if stage := queryString(t, dbPath, `select failure_stage from deployments where id = ?`, badAttempt); stage != string(compose.DeployStageWaitServices) {
		t.Fatalf("failed replacement stage = %q, want %q", stage, compose.DeployStageWaitServices)
	}
	badContainer := recoveryContainer(t, project)
	if badContainer == goodContainer {
		t.Fatal("failed replacement kept the original container; acceptance did not exercise Compose replacement")
	}
	assertRemoteMain(t, repoPath, badCommit)
	assertRecoveryRuntime(t, badContainer, port, "B", "unhealthy", seed)
	assertRecoveryHealthIdentity(t, cliPath, cliEnv, appName, badCommit, badAttempt, app.DeploymentStatusFailed, goodCommit, goodAttempt, badAttempt)

	// Even selecting a historically successful commit can fail under changed prerequisites.
	runCommandInput(t, "", cliEnv, "blocked", cliPath, "config", "set", appName, "RECOVERY_READY")
	rollbackOutput := runGitOutput(t, sourceDir, pushEnv, "push", "--force-with-lease=refs/heads/main:"+badCommit, "sshdock", goodCommit+":refs/heads/main")
	rollbackAttempt := recoveryAttemptID(t, rollbackOutput)
	if rollbackAttempt == goodAttempt || rollbackAttempt == badAttempt {
		t.Fatal("rollback did not create a new exact attempt")
	}
	waitForRecoveryAttempt(t, dbPath, appName, rollbackAttempt, goodCommit, app.DeploymentStatusFailed)
	if stage := queryString(t, dbPath, `select failure_stage from deployments where id = ?`, rollbackAttempt); stage != string(compose.DeployStageWaitServices) {
		t.Fatalf("failed rollback stage = %q, want %q", stage, compose.DeployStageWaitServices)
	}
	rollbackContainer := recoveryContainer(t, project)
	if rollbackContainer == badContainer {
		t.Fatal("failed rollback did not replace B")
	}
	assertRemoteMain(t, repoPath, goodCommit)
	assertRecoveryRuntime(t, rollbackContainer, port, "A", "unhealthy", seed)
	assertRecoveryHealthIdentity(t, cliPath, cliEnv, appName, goodCommit, rollbackAttempt, app.DeploymentStatusFailed, goodCommit, goodAttempt, rollbackAttempt)
	if got := queryString(t, dbPath, `select release_id from deployments where id = ?`, rollbackAttempt); got != app.ReleaseID(appName, goodCommit) {
		t.Fatalf("rollback release = %q, want reused commit release", got)
	}

	// Correct the prerequisite, then explicitly retry current main without another ref change.
	runCommandInput(t, "", cliEnv, "ready", cliPath, "config", "set", appName, "RECOVERY_READY")
	runCommand(t, "", cliEnv, cliPath, "apps", "redeploy", appName)
	retryAttempt := queryString(t, dbPath, `select id from deployments where app_id = ? order by rtrim(started_at, 'Z') desc, id desc limit 1`, appName)
	if retryAttempt == goodAttempt || retryAttempt == badAttempt || retryAttempt == rollbackAttempt {
		t.Fatal("redeploy did not create a new exact attempt")
	}
	if got := queryString(t, dbPath, `select commit_sha || ':' || trigger || ':' || status from deployments where id = ?`, retryAttempt); got != goodCommit+":redeploy:succeeded" {
		t.Fatalf("current-main retry = %q", got)
	}
	assertRemoteMain(t, repoPath, goodCommit)
	assertRecoveryRuntime(t, recoveryContainer(t, project), port, "A", "healthy", seed)
	assertRecoveryHealthIdentity(t, cliPath, cliEnv, appName, goodCommit, retryAttempt, app.DeploymentStatusSucceeded, goodCommit, retryAttempt, rollbackAttempt)
	if attempts := queryString(t, dbPath, `select count(*) from deployments where app_id = ?`, appName); attempts != "4" {
		t.Fatalf("deployment attempts = %q, want four explicit attempts", attempts)
	}
	if automatic := queryString(t, dbPath, `select count(*) from deployments where app_id = ? and trigger not in ('push', 'redeploy')`, appName); automatic != "0" {
		t.Fatalf("unexpected automatic recovery attempts = %q", automatic)
	}
}

func recoveryDockerCompose(port int, version string, healthy bool) string {
	healthCommand := `["CMD", "false"]`
	if healthy {
		healthCommand = `["CMD-SHELL", "test \"$$RECOVERY_READY\" = ready && wget -q -O /dev/null http://127.0.0.1:8080/"]`
	}
	return fmt.Sprintf(`services:
  web:
    image: %s
    environment:
      RECOVERY_READY: ${RECOVERY_READY:-ready}
    command:
      - sh
      - -c
      - |
        mkdir -p /www
        printf '%s\n' > /www/index.html
        test -f /data/sentinel || printf initial > /data/sentinel
        exec httpd -f -p 8080 -h /www
    ports:
      - "127.0.0.1:%d:8080"
    volumes:
      - data:/data
    healthcheck:
      test: %s
      interval: 1s
      timeout: 1s
      start_period: 1s
      retries: 1
volumes:
  data:
`, lifecycleBusyboxImage, version, port, healthCommand)
}

func recoveryContainer(t *testing.T, project string) string {
	t.Helper()
	output := runCommand(t, "", nil, "docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+project, "--filter", "label=com.docker.compose.service=web")
	ids := strings.Fields(output)
	if len(ids) != 1 {
		t.Fatalf("project web containers = %q, want exactly one", output)
	}
	return ids[0]
}

func assertRecoveryRuntime(t *testing.T, container string, port int, version, health, seed string) {
	t.Helper()
	state := strings.TrimSpace(runCommand(t, "", nil, "docker", "inspect", "--format", "{{.State.Status}} {{.State.Health.Status}}", container))
	if state != "running "+health {
		t.Fatalf("container state = %q, want running %s", state, health)
	}
	if got := strings.TrimSpace(runCommand(t, "", nil, "docker", "exec", container, "cat", "/data/sentinel")); got != seed {
		t.Fatalf("named volume sentinel = %q, want %q", got, seed)
	}
	// This isolated fixture has no public domain: inspect its direct response separately from health.
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != version {
		t.Fatalf("application response = %d %q, want 200 %s", response.StatusCode, body, version)
	}
}

func assertRecoveryHealthIdentity(t *testing.T, cliPath string, cliEnv []string, appName, currentMain, latestID string, latestStatus app.DeploymentStatus, successfulCommit, successfulID, failedID string) {
	t.Helper()
	output := runCommand(t, "", cliEnv, cliPath, "apps", "health", appName, "--json")
	var envelope struct {
		Data  app.HealthReport `json:"data"`
		Error json.RawMessage  `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode health: %v\n%s", err, output)
	}
	got := envelope.Data
	if string(envelope.Error) != "null" || got.CurrentMainCommit != currentMain || got.LatestDeploymentID != latestID || got.LatestDeploymentCommit != currentMain || got.LatestDeploymentStatus != latestStatus || got.LastSuccessfulDeploymentID != successfulID || got.LastSuccessfulDeploymentCommit != successfulCommit || got.LastSuccessfulReleaseID != app.ReleaseID(appName, successfulCommit) || got.LastFailureDeploymentID != failedID {
		t.Fatalf("health identities disagree with current main, latest exact attempt or preserved history:\n%s", output)
	}
}

func recoveryAttemptID(t *testing.T, output string) string {
	t.Helper()
	var id string
	for _, line := range strings.Split(output, "\n") {
		for _, marker := range []string{"deploy: queued ", "deploy: following "} {
			_, suffix, found := strings.Cut(line, marker)
			if !found {
				continue
			}
			fields := strings.Fields(suffix)
			if len(fields) == 0 || !strings.HasPrefix(fields[0], "dep_") || id != "" && id != fields[0] {
				t.Fatalf("invalid or conflicting exact attempt IDs:\n%s", output)
			}
			id = fields[0]
		}
	}
	if id == "" {
		t.Fatalf("push output has no new attempt ID:\n%s", output)
	}
	return id
}

func waitForRecoveryAttempt(t *testing.T, dbPath, appName, id, commit string, want app.DeploymentStatus) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var gotCommit, status string
		var terminalEvents int
		err := db.QueryRow(`select commit_sha, status, (select count(*) from events where app_id = ? and id = ?) from deployments where app_id = ? and id = ?`, appName, gitrecv.EventID(id, string(want)), appName, id).Scan(&gotCommit, &status, &terminalEvents)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		if err == nil {
			if gotCommit != commit || status != string(want) && (status == "succeeded" || status == "failed") {
				t.Fatalf("exact attempt %s commit=%s status=%s; want commit=%s status=%s", id, gotCommit, status, commit, want)
			}
			if status == string(want) && terminalEvents == 1 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("exact attempt %s did not finish %s with its terminal event", id, want)
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

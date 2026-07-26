//go:build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestServerPushImageServiceEndToEnd(t *testing.T) {
	paths := setupBootstrappedServerPush(t, "fake")

	appName := "server-image-app"
	commitSHA, output := pushComposeAppThroughSSHWithOutput(t, paths, appName, map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	})
	queued := regexp.MustCompile(`deploy: queued (dep_[^ ]+)`).FindStringSubmatch(output)
	if len(queued) != 2 {
		t.Fatalf("push output missing queued deployment ID:\n%s", output)
	}
	for _, want := range []string{"deploy: following " + queued[1], "deploy: daemon started", "deploy: succeeded"} {
		if !strings.Contains(output, want) {
			t.Fatalf("push output missing %q:\n%s", want, output)
		}
	}

	dbPath := filepath.Join(paths.dataDir, "sshdock.db")
	assertAppStatus(t, dbPath, appName, app.AppStatusHealthy)
	assertReleaseStatus(t, dbPath, app.ReleaseID(appName, commitSHA), app.ReleaseStatusSucceeded)
	status, err := deploymentStatusForCommit(dbPath, appName, commitSHA, app.DeploymentTriggerPush)
	if err != nil {
		t.Fatalf("deploymentStatus: %v", err)
	}
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
	assertEventTypesContain(t, dbPath, appName, []string{"git.ref_accepted", "deploy.queued", "deploy.started", "deploy.succeeded", "route.auto_skipped"})
}

func TestServerPushClientDisconnectDoesNotInterruptDeployment(t *testing.T) {
	t.Setenv("SSHDOCK_FAKE_COMPOSE_DEPLOY_DELAY", "2s")
	paths := setupBootstrappedServerPush(t, "fake")

	appName := "server-detached-client-app"
	push := prepareComposeAppPush(t, paths, composePushRequest{AppName: appName, Files: map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	}})
	output := newPushCommandSignalWriter("deploy: daemon started")
	command := exec.Command("git", "push", "sshdock", "main")
	command.Dir = push.sourceDir
	command.Env = push.env
	command.Stdout = output
	command.Stderr = output
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatalf("start git push: %v", err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			_ = command.Wait()
		}
	})
	select {
	case <-output.matched:
	case <-time.After(5 * time.Second):
		t.Fatalf("git push never attached to live deployment output:\n%s", output.String())
	}

	dbPath := filepath.Join(paths.dataDir, "sshdock.db")
	status := waitForVisibleDeploymentStatus(t, dbPath, appName, push.commitSHA)
	if status == string(app.DeploymentStatusSucceeded) || status == string(app.DeploymentStatusFailed) {
		t.Fatalf("deployment status before client disconnect = %q, want queued or active", status)
	}

	// When
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatalf("disconnect git push client: %v", err)
	}
	disconnected := make(chan error, 1)
	go func() { disconnected <- command.Wait() }()
	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-disconnected
		t.Fatal("git push process group did not stop after Ctrl-C-style disconnect")
	}

	// Then
	waitForDeploymentTerminal(t, dbPath, appName, push.commitSHA)
	assertAppStatus(t, dbPath, appName, app.AppStatusHealthy)
}

func TestServerPushBuildServiceDockerEndToEnd(t *testing.T) {
	requireDocker(t)
	paths := setupBootstrappedServerPush(t, "docker")

	appName := "server-build-app"
	projectName := compose.ProjectName(appName)
	hostPort := freeLocalPort(t)
	releaseImagePrefix := "sshdock/" + appName + "/"
	legacyImagesBefore := countImageRepositoriesWithPrefix(
		runCommand(t, "", nil, "docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}"),
		releaseImagePrefix,
	)
	commitSHA := pushComposeAppThroughSSH(t, paths, appName, map[string]string{
		"compose.yml":       fmt.Sprintf("services:\n  base:\n    build: .\n  web:\n    extends:\n      service: ${BASE_SERVICE:-base}\n    ports: [\"0.0.0.0:%d:80\"]\n    volumes: [\"./public:/usr/share/nginx/html:ro\"]\n  debug:\n    profiles: [debug]\n    build: ./missing-debug\n", hostPort),
		"Dockerfile":        "FROM nginx:alpine\n",
		"public/index.html": "native compose build\n",
	})
	worktreePath := filepath.Join(paths.dataDir, "apps", appName, "worktree")
	composePath := filepath.Join(worktreePath, "compose.yml")
	t.Cleanup(func() {
		_ = runCommandNoFail(worktreePath, nil, "docker", "compose", "-f", composePath, "-p", projectName, "down", "-v", "--remove-orphans")
	})

	if matches, err := filepath.Glob(filepath.Join(worktreePath, ".sshdock", "release-*.compose.yml")); err != nil {
		t.Fatalf("Glob release overrides: %v", err)
	} else if len(matches) != 0 {
		t.Fatalf("release overrides = %#v, want none", matches)
	}

	output := runCommand(t, worktreePath, nil, "docker", "compose", "-f", composePath, "-p", projectName, "ps", "--format", "json")
	if !strings.Contains(output, `"Service":"web"`) && !strings.Contains(output, `"Name":"web"`) {
		t.Fatalf("docker compose ps output missing web service:\n%s", output)
	}
	if !strings.Contains(output, `"State":"running"`) {
		t.Fatalf("docker compose ps output missing running state:\n%s", output)
	}
	images := runCommand(t, worktreePath, nil, "docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}")
	legacyImagesAfter := countImageRepositoriesWithPrefix(images, releaseImagePrefix)
	if legacyImagesAfter != legacyImagesBefore {
		t.Fatalf("SSHDock release image count changed from %d to %d:\n%s", legacyImagesBefore, legacyImagesAfter, images)
	}

	dbPath := filepath.Join(paths.dataDir, "sshdock.db")
	assertAppStatus(t, dbPath, appName, app.AppStatusHealthy)
	assertReleaseStatus(t, dbPath, app.ReleaseID(appName, commitSHA), app.ReleaseStatusSucceeded)
	status, err := deploymentStatusForCommit(dbPath, appName, commitSHA, app.DeploymentTriggerPush)
	if err != nil {
		t.Fatalf("deploymentStatus: %v", err)
	}
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
	assertEventTypes(t, dbPath, appName, []string{"git.ref_accepted", "deploy.started", "deploy.warning", "deploy.warning", "deploy.succeeded"})
	assertEventMessageContains(t, dbPath, appName, "publishes 0.0.0.0:")
	assertEventMessageContains(t, dbPath, appName, "uses host bind mount")
	assertEventMessageContains(t, dbPath, appName, "does not sandbox this configuration")
}

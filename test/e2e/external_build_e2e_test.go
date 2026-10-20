//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestExternalBuildRegistryEndToEnd(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run the external-build registry test")
	}
	requireDocker(t)
	paths := setupBootstrappedServerPush(t, "docker")
	operator := startDashboardSSHServer(t, paths, requireCommandOrSkip(t, "sshd"), requireCommandOrSkip(t, "ssh-keygen"))
	operate := func(args ...string) string {
		return runCommand(t, "", nil, "ssh", append(dashboardSSHArgs(paths, operator, false), args...)...)
	}
	registryPort, appPort := freeLocalPort(t), freeLocalPort(t)
	registry := fmt.Sprintf("sshdock-registry-e2e-%d", registryPort)
	repository := fmt.Sprintf("127.0.0.1:%d/external", registryPort)
	appName := fmt.Sprintf("external-%d", appPort)
	runCommand(t, "", nil, "docker", "run", "-d", "--name", registry, "-p", fmt.Sprintf("127.0.0.1:%d:5000", registryPort), "registry:2")
	t.Cleanup(func() { _ = runCommandNoFail("", nil, "docker", "rm", "-f", "-v", registry) })
	runCommand(t, "", nil, "curl", "--fail", "--retry", "20", "--retry-connrefused", "--retry-delay", "1", fmt.Sprintf("http://127.0.0.1:%d/v2/", registryPort))

	// Both immutable-by-convention tags exist before either Git push. The target
	// checkout has no Dockerfile or build context and its .env tries to spoof A.
	push := prepareComposeAppPush(t, paths, composePushRequest{AppName: appName, Files: map[string]string{
		"compose.yml": fmt.Sprintf(`services:
  web:
    image: %s:${SSHDOCK_GIT_SHA:?deploy a commit}
    environment:
      REVISION: ${SSHDOCK_GIT_SHA:?deploy a commit}
    ports: ["127.0.0.1:%d:8080"]
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080"]
      interval: 1s
      timeout: 1s
      retries: 20
`, repository, appPort),
		"revision.txt": "A\n",
	}})
	a := push.commitSHA
	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(push.sourceDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(".env", "SSHDOCK_GIT_SHA="+a+"\n")
	writeFile("revision.txt", "B\n")
	runGit(t, push.sourceDir, nil, "add", ".")
	runGit(t, push.sourceDir, nil, "commit", "-m", "Revision B")
	b := strings.TrimSpace(runGitOutput(t, push.sourceDir, nil, "rev-parse", "HEAD"))
	buildDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte("FROM busybox:1.37\nARG REVISION\nRUN mkdir /www && printf '%s' \"$REVISION\" > /www/index.html\nUSER 65534:65534\nCMD [\"httpd\", \"-f\", \"-p\", \"8080\", \"-h\", \"/www\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, sha := range []string{a, b} {
		image := repository + ":" + sha
		runCommand(t, buildDir, nil, "docker", "build", "--build-arg", "REVISION="+sha, "-t", image, ".")
		runCommand(t, "", nil, "docker", "push", image)
		// A shared local Docker daemon must still pull from the test registry.
		runCommand(t, "", nil, "docker", "image", "rm", image)
		t.Cleanup(func() { _ = runCommandNoFail("", nil, "docker", "image", "rm", image) })
	}
	worktree := filepath.Join(paths.dataDir, "apps", appName, "worktree")
	project := compose.ProjectName(appName)
	cleanupEnv := append(os.Environ(), "SSHDOCK_GIT_SHA="+a)
	t.Cleanup(func() {
		_ = runCommandNoFail(worktree, cleanupEnv, "docker", "compose", "-p", project, "down", "--remove-orphans")
	})
	dbPath := filepath.Join(paths.dataDir, "sshdock.db")
	assertRevision := func(sha string) {
		t.Helper()
		body := runCommand(t, "", nil, "curl", "--fail", "--silent", fmt.Sprintf("http://127.0.0.1:%d", appPort))
		if body != sha {
			t.Fatalf("served image revision = %q, want %s", body, sha)
		}
		container := strings.TrimSpace(runCommand(t, "", nil, "docker", "ps", "-q", "--filter", "label=com.docker.compose.project="+project, "--filter", "label=com.docker.compose.service=web"))
		image := strings.TrimSpace(runCommand(t, "", nil, "docker", "inspect", "--format", "{{.Config.Image}}", container))
		if image != repository+":"+sha {
			t.Fatalf("container image = %q, want revision %s", image, sha)
		}
	}
	for _, sha := range []string{a, b} {
		output := runGitOutput(t, push.sourceDir, push.env, "push", "sshdock", sha+":refs/heads/main")
		if !strings.Contains(output, "deploy: succeeded") {
			t.Fatalf("push did not deploy %s:\n%s", sha, output)
		}
		assertAppStatus(t, dbPath, appName, app.AppStatusHealthy)
		assertRevision(sha)
		for _, action := range []string{"exec", "run"} {
			output := operate("apps", action, appName, "web", "--", "printenv", "REVISION")
			if !strings.Contains(output, sha) {
				t.Fatalf("%s missing revision %s:\n%s", action, sha, output)
			}
		}
		if _, err := os.Stat(filepath.Join(worktree, "Dockerfile")); !os.IsNotExist(err) {
			t.Fatalf("target unexpectedly contains a Dockerfile: %v", err)
		}
		if output := operate("deployments", "logs", appName); !strings.Contains(output, "No services to build") {
			t.Fatalf("target did not report an image-only deployment:\n%s", output)
		}
	}

	// An unpublished revision records a pull failure without relabeling B as C.
	writeFile("revision.txt", "unpublished\n")
	runGit(t, push.sourceDir, nil, "add", ".")
	runGit(t, push.sourceDir, nil, "commit", "-m", "Missing image")
	missing := strings.TrimSpace(runGitOutput(t, push.sourceDir, nil, "rev-parse", "HEAD"))
	output := runGitOutput(t, push.sourceDir, push.env, "push", "sshdock", missing+":refs/heads/main")
	if !strings.Contains(output, "deploy: failed stage=pull images") {
		t.Fatalf("missing image did not fail at pull:\n%s", output)
	}
	assertRemoteMain(t, filepath.Join(paths.dataDir, "apps", appName, "repo.git"), missing)
	assertRevision(b)

	runGit(t, push.sourceDir, push.env, "push", "--force", "sshdock", a+":refs/heads/main")
	operate("apps", "redeploy", appName)
	assertRevision(a)
	if output := operate("apps", "health", appName); !strings.Contains(output, "status: healthy") || !strings.Contains(output, "services: 1 running, 0 attention") || !strings.Contains(output, "succeeded commit="+a) {
		t.Fatalf("recovered health:\n%s", output)
	}
}

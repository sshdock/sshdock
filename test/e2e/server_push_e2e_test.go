//go:build e2e

package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
	_ "modernc.org/sqlite"
)

func TestServerPushImageServiceEndToEnd(t *testing.T) {
	paths := setupBootstrappedServerPush(t, "fake")

	appName := "server-image-app"
	commitSHA := pushComposeAppThroughSSH(t, paths, appName, map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	})

	dbPath := filepath.Join(paths.dataDir, "sshdock.db")
	assertAppStatus(t, dbPath, appName, app.AppStatusHealthy)
	assertReleaseStatus(t, dbPath, "rel_"+shortSHA(commitSHA), app.ReleaseStatusSucceeded)
	status, err := deploymentStatus(dbPath, "dep_"+shortSHA(commitSHA))
	if err != nil {
		t.Fatalf("deploymentStatus: %v", err)
	}
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
	assertEventTypes(t, dbPath, appName, []string{"deploy.started", "deploy.succeeded"})
}

func TestServerPushBuildServiceDockerEndToEnd(t *testing.T) {
	requireDocker(t)
	paths := setupBootstrappedServerPush(t, "docker")

	appName := "server-build-app"
	projectName := compose.ProjectName(appName)
	commitSHA := pushComposeAppThroughSSH(t, paths, appName, map[string]string{
		"compose.yml": "services:\n  web:\n    build: .\n",
		"Dockerfile":  "FROM nginx:alpine\n",
	})
	worktreePath := filepath.Join(paths.dataDir, "apps", appName, "worktree")
	composePath := filepath.Join(worktreePath, "compose.yml")
	overridePath := filepath.Join(worktreePath, ".sshdock", "release-"+commitSHA+".compose.yml")
	t.Cleanup(func() {
		_ = runCommandNoFail(worktreePath, nil, "docker", "compose", "-f", composePath, "-f", overridePath, "-p", projectName, "down", "-v", "--remove-orphans")
		_ = runCommandNoFail(worktreePath, nil, "docker", "image", "rm", "sshdock/"+appName+"/web:"+commitSHA)
		_ = runCommandNoFail(worktreePath, nil, "docker", "image", "rm", "sshdock/"+appName+"/web:latest")
	})

	override := readFile(t, overridePath)
	wantImage := "sshdock/" + appName + "/web:" + commitSHA
	if !strings.Contains(override, wantImage) {
		t.Fatalf("release override missing %q:\n%s", wantImage, override)
	}

	output := runCommand(t, worktreePath, nil, "docker", "compose", "-f", composePath, "-f", overridePath, "-p", projectName, "ps", "--format", "json")
	if !strings.Contains(output, `"Service":"web"`) && !strings.Contains(output, `"Name":"web"`) {
		t.Fatalf("docker compose ps output missing web service:\n%s", output)
	}
	if !strings.Contains(output, `"State":"running"`) {
		t.Fatalf("docker compose ps output missing running state:\n%s", output)
	}

	dbPath := filepath.Join(paths.dataDir, "sshdock.db")
	assertAppStatus(t, dbPath, appName, app.AppStatusHealthy)
	assertReleaseStatus(t, dbPath, "rel_"+shortSHA(commitSHA), app.ReleaseStatusSucceeded)
	status, err := deploymentStatus(dbPath, "dep_"+shortSHA(commitSHA))
	if err != nil {
		t.Fatalf("deploymentStatus: %v", err)
	}
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
	assertEventTypes(t, dbPath, appName, []string{"deploy.started", "deploy.succeeded"})
}

type serverPushPaths struct {
	tmp                         string
	installBinDir               string
	dataDir                     string
	clientKeyPath               string
	dashboardAuthorizedKeysPath string
	sshPort                     int
	sshUser                     string
}

func setupBootstrappedServerPush(t *testing.T, composeRunner string) serverPushPaths {
	t.Helper()
	requireGit(t)
	sshdPath := requireCommandOrSkip(t, "sshd")
	sshKeygenPath := requireCommandOrSkip(t, "ssh-keygen")

	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("current user: %v", err)
	}
	if currentUser.Username == "" {
		t.Skip("current user name is required for server push e2e")
	}

	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sourceBinDir := filepath.Join(tmp, "source-bin")
	if err := os.MkdirAll(sourceBinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source bin: %v", err)
	}
	runCommand(t, root, nil, "go", "build", "-o", filepath.Join(sourceBinDir, "sshdock"), "./cmd/sshdock")
	runCommand(t, root, nil, "go", "build", "-o", filepath.Join(sourceBinDir, "sshdockd"), "./cmd/sshdockd")

	fakeBinDir := filepath.Join(tmp, "fake-bin")
	fakeLogPath := filepath.Join(tmp, "fake-commands.log")
	writeBootstrapFakeCommands(t, fakeBinDir)

	installRoot := filepath.Join(tmp, "root")
	bootstrapEnv := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_TAG=test-local",
		"SSHDOCK_BOOTSTRAP_ROOT="+installRoot,
		"SSHDOCK_BOOTSTRAP_SOURCE_BIN_DIR="+sourceBinDir,
		"SSHDOCK_BOOTSTRAP_SKIP_CHOWN=1",
		"SSHDOCK_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
	)
	runCommand(t, root, bootstrapEnv, "bash", "scripts/bootstrap.sh")

	installBinDir := filepath.Join(installRoot, "usr", "local", "bin")
	dataDir := filepath.Join(installRoot, "var", "lib", "sshdock")
	authorizedKeysPath := filepath.Join(dataDir, "git", ".ssh", "authorized_keys")
	dashboardAuthorizedKeysPath := filepath.Join(dataDir, "dashboard", ".ssh", "authorized_keys")
	clientKeyPath := filepath.Join(tmp, "client_ed25519")
	runCommand(t, tmp, nil, sshKeygenPath, "-t", "ed25519", "-N", "", "-f", clientKeyPath)
	publicKey := readFile(t, clientKeyPath+".pub")

	receiveCommand := fmt.Sprintf("env PATH=%s%c%s SSHDOCK_DATA_DIR=%s SSHDOCK_COMPOSE_RUNNER=%s %s git-receive",
		installBinDir,
		os.PathListSeparator,
		os.Getenv("PATH"),
		dataDir,
		composeRunner,
		filepath.Join(installBinDir, "sshdockd"),
	)
	dashboardCommand := fmt.Sprintf("env PATH=%s%c%s SSHDOCK_DATA_DIR=%s SSHDOCK_COMPOSE_RUNNER=fake SSHDOCK_FAKE_COMPOSE_SERVICES=web:running SSHDOCK_FAKE_COMPOSE_LOGS=first-dashboard-log %s dashboard",
		installBinDir,
		os.PathListSeparator,
		os.Getenv("PATH"),
		dataDir,
		filepath.Join(installBinDir, "sshdockd"),
	)
	cliEnv := append(os.Environ(),
		"PATH="+installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_GIT_AUTHORIZED_KEYS_PATH="+authorizedKeysPath,
		"SSHDOCK_GIT_RECEIVE_COMMAND="+receiveCommand,
		"SSHDOCK_DASHBOARD_AUTHORIZED_KEYS_PATH="+dashboardAuthorizedKeysPath,
		"SSHDOCK_DASHBOARD_COMMAND="+dashboardCommand,
		"SSHDOCK_COMPOSE_RUNNER="+composeRunner,
	)
	runCommandInput(t, root, cliEnv, publicKey, filepath.Join(installBinDir, "sshdock"), "ssh-keys", "add", "admin")

	hostKeyPath := filepath.Join(tmp, "host_ed25519")
	runCommand(t, tmp, nil, sshKeygenPath, "-t", "ed25519", "-N", "", "-f", hostKeyPath)
	port := freeLocalPort(t)
	sshdConfigPath := filepath.Join(tmp, "sshd_config")
	sshdLogPath := filepath.Join(tmp, "sshd.log")
	sshdConfig := fmt.Sprintf(`
Port %d
ListenAddress 127.0.0.1
HostKey %s
PidFile %s
AuthorizedKeysFile %s
PasswordAuthentication no
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
PubkeyAuthentication yes
StrictModes no
AllowUsers %s
LogLevel ERROR
`, port, hostKeyPath, filepath.Join(tmp, "sshd.pid"), authorizedKeysPath, currentUser.Username)
	if err := os.WriteFile(sshdConfigPath, []byte(sshdConfig), 0o600); err != nil {
		t.Fatalf("WriteFile sshd_config: %v", err)
	}
	if output, err := exec.Command(sshdPath, "-t", "-f", sshdConfigPath).CombinedOutput(); err != nil {
		t.Skipf("OpenSSH server config is not usable in this environment: %v\n%s", err, output)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sshd := exec.CommandContext(ctx, sshdPath, "-D", "-e", "-f", sshdConfigPath)
	logFile, err := os.Create(sshdLogPath)
	if err != nil {
		cancel()
		t.Fatalf("Create sshd log: %v", err)
	}
	sshd.Stdout = logFile
	sshd.Stderr = logFile
	if err := sshd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		t.Skipf("start sshd: %v", err)
	}
	waitForTCP(t, "127.0.0.1", port, sshdLogPath)

	cancelSSHD := func() {
		cancel()
		_ = sshd.Wait()
		_ = logFile.Close()
	}
	t.Cleanup(cancelSSHD)

	return serverPushPaths{
		tmp:                         tmp,
		installBinDir:               installBinDir,
		dataDir:                     dataDir,
		clientKeyPath:               clientKeyPath,
		dashboardAuthorizedKeysPath: dashboardAuthorizedKeysPath,
		sshPort:                     port,
		sshUser:                     currentUser.Username,
	}
}

func pushComposeAppThroughSSH(t *testing.T, paths serverPushPaths, appName string, files map[string]string) string {
	t.Helper()
	sourceDir := filepath.Join(paths.tmp, "source-"+appName)
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source: %v", err)
	}
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	for name, content := range files {
		path := filepath.Join(sourceDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	runGit(t, sourceDir, nil, "add", ".")
	runGit(t, sourceDir, nil, "commit", "-m", "initial compose app")
	commitSHA := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	runGit(t, sourceDir, nil, "remote", "add", "sshdock", paths.sshUser+"@127.0.0.1:"+appName+".git")

	sshPath := requireCommandOrSkip(t, "ssh")
	sshCommand := fmt.Sprintf("%s -p %d -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", sshPath, paths.sshPort, paths.clientKeyPath)
	pushEnv := append(os.Environ(),
		"PATH="+paths.installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_SSH_COMMAND="+sshCommand,
		"SSHDOCK_DATA_DIR="+paths.dataDir,
	)
	runGit(t, sourceDir, pushEnv, "push", "sshdock", "main")
	return commitSHA
}

func writeBootstrapFakeCommands(t *testing.T, fakeBinDir string) {
	t.Helper()
	writeFakeCommand(t, fakeBinDir, "docker", `#!/bin/sh
printf 'docker %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "caddy", `#!/bin/sh
printf 'caddy %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "sudo", `#!/bin/sh
printf 'sudo %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "systemctl", `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "id", `#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = "-u" ]; then
	echo 0
	exit 0
fi
printf 'id %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 1
`)
	writeFakeCommand(t, fakeBinDir, "useradd", `#!/bin/sh
printf 'useradd %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "usermod", `#!/bin/sh
printf 'usermod %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "visudo", `#!/bin/sh
printf 'visudo %s\n' "$*" >> "$SSHDOCK_BOOTSTRAP_FAKE_LOG"
exit 0
`)
}

func assertAppStatus(t *testing.T, dbPath string, appID string, want app.AppStatus) {
	t.Helper()
	got := queryString(t, dbPath, `select status from apps where id = ?`, appID)
	if got != string(want) {
		t.Fatalf("app %s status = %q, want %q", appID, got, want)
	}
}

func assertReleaseStatus(t *testing.T, dbPath string, releaseID string, want app.ReleaseStatus) {
	t.Helper()
	got := queryString(t, dbPath, `select status from releases where id = ?`, releaseID)
	if got != string(want) {
		t.Fatalf("release %s status = %q, want %q", releaseID, got, want)
	}
}

func assertEventTypes(t *testing.T, dbPath string, appID string, want []string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`select type from events where app_id = ? order by created_at, id`, appID)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatalf("scan event type: %v", err)
		}
		got = append(got, eventType)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("events rows: %v", err)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("event types = %#v, want %#v", got, want)
	}
}

func queryString(t *testing.T, dbPath string, query string, args ...any) string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	var value string
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query string: %v", err)
	}
	return value
}

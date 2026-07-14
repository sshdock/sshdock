//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestCLIServerDomainAndSSHKeysEndToEnd(t *testing.T) {
	requireGit(t)

	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}
	sshdockPath := filepath.Join(binDir, "sshdock")
	runCommand(t, root, nil, "go", "build", "-o", sshdockPath, "./cmd/sshdock")

	dataDir := filepath.Join(tmp, "data")
	authorizedKeysPath := filepath.Join(tmp, "git", ".ssh", "authorized_keys")
	env := append(os.Environ(),
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_GIT_HOST=env.example.com",
		"SSHDOCK_GIT_AUTHORIZED_KEYS_PATH="+authorizedKeysPath,
		"SSHDOCK_GIT_RECEIVE_COMMAND=/usr/local/bin/sshdockd git-receive",
	)

	runCommand(t, root, env, sshdockPath, "server", "domain", "set", "example.com")
	output := runCommand(t, root, env, sshdockPath, "apps", "create", "my-app")
	if !strings.Contains(output, "git remote add sshdock git@sshdock.example.com:my-app.git") {
		t.Fatalf("apps create output missing persisted host:\n%s", output)
	}

	publicKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com\n"
	output = runCommandInput(t, root, env, publicKey, sshdockPath, "ssh-keys", "add", "admin")
	if !strings.Contains(output, "added SSH key admin") {
		t.Fatalf("ssh-keys output = %s", output)
	}
	authorizedKeys := readFile(t, authorizedKeysPath)
	for _, want := range []string{
		`command="exec /usr/local/bin/sshdockd git-receive"`,
		`no-pty`,
		`no-port-forwarding`,
		`no-agent-forwarding`,
		`no-X11-forwarding`,
		strings.TrimSpace(publicKey),
	} {
		if !strings.Contains(authorizedKeys, want) {
			t.Fatalf("authorized_keys missing %q:\n%s", want, authorizedKeys)
		}
	}
}

func TestOpenSSHGitReceivePushToCreateEndToEnd(t *testing.T) {
	requireGit(t)
	sshdPath := requireCommandOrSkip(t, "sshd")
	sshPath := requireCommandOrSkip(t, "ssh")
	sshKeygenPath := requireCommandOrSkip(t, "ssh-keygen")

	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("current user: %v", err)
	}
	if currentUser.Username == "" {
		t.Skip("current user name is required for OpenSSH e2e")
	}

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

	clientKeyPath := filepath.Join(tmp, "client_ed25519")
	runCommand(t, tmp, nil, sshKeygenPath, "-t", "ed25519", "-N", "", "-f", clientKeyPath)
	publicKey := readFile(t, clientKeyPath+".pub")

	hostKeyPath := filepath.Join(tmp, "host_ed25519")
	runCommand(t, tmp, nil, sshKeygenPath, "-t", "ed25519", "-N", "", "-f", hostKeyPath)

	dataDir := filepath.Join(tmp, "data")
	authorizedKeysPath := filepath.Join(tmp, "authorized_keys")
	receiveCommand := fmt.Sprintf("env PATH=%s%c%s SSHDOCK_DATA_DIR=%s SSHDOCK_COMPOSE_RUNNER=fake %s git-receive",
		binDir,
		os.PathListSeparator,
		os.Getenv("PATH"),
		dataDir,
		sshdockdPath,
	)
	env := append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_GIT_AUTHORIZED_KEYS_PATH="+authorizedKeysPath,
		"SSHDOCK_GIT_RECEIVE_COMMAND="+receiveCommand,
		"SSHDOCK_COMPOSE_RUNNER=fake",
	)
	runCommandInput(t, root, env, publicKey, sshdockPath, "ssh-keys", "add", "admin")

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
	defer cancel()
	sshd := exec.CommandContext(ctx, sshdPath, "-D", "-e", "-f", sshdConfigPath)
	logFile, err := os.Create(sshdLogPath)
	if err != nil {
		t.Fatalf("Create sshd log: %v", err)
	}
	defer logFile.Close()
	sshd.Stdout = logFile
	sshd.Stderr = logFile
	if err := sshd.Start(); err != nil {
		t.Skipf("start sshd: %v", err)
	}
	defer func() {
		cancel()
		_ = sshd.Wait()
	}()
	waitForTCP(t, "127.0.0.1", port, sshdLogPath)

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
	runGit(t, sourceDir, nil, "commit", "-m", "initial openssh compose app")
	commitSHA := strings.TrimSpace(runGitOutput(t, sourceDir, nil, "rev-parse", "HEAD"))
	runGit(t, sourceDir, nil, "remote", "add", "sshdock", currentUser.Username+"@127.0.0.1:ssh-app.git")

	sshCommand := fmt.Sprintf("%s -p %d -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", sshPath, port, clientKeyPath)
	pushEnv := append(env, "GIT_SSH_COMMAND="+sshCommand)
	runGit(t, sourceDir, pushEnv, "push", "sshdock", "main")

	status, err := deploymentStatusForCommit(filepath.Join(dataDir, "sshdock.db"), "ssh-app", commitSHA, app.DeploymentTriggerPush)
	if err != nil {
		t.Fatalf("deploymentStatus: %v\nsshd log:\n%s", err, readFile(t, sshdLogPath))
	}
	if status != string(app.DeploymentStatusSucceeded) {
		t.Fatalf("deployment status = %q", status)
	}
}

func runCommandInput(t *testing.T, dir string, env []string, input string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func requireCommandOrSkip(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is required for OpenSSH e2e: %v", name, err)
	}
	return path
}

func freeLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForTCP(t *testing.T, host string, port int, logPath string) {
	t.Helper()
	address := fmt.Sprintf("%s:%d", host, port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for sshd at %s\nsshd log:\n%s", address, readFile(t, logPath))
}

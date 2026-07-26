//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
)

type serverPushPaths struct {
	tmp                        string
	installBinDir              string
	dataDir                    string
	clientKeyPath              string
	operatorAuthorizedKeysPath string
	sshPort                    int
	sshUser                    string
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
	operatorAuthorizedKeysPath := filepath.Join(dataDir, ".ssh", "authorized_keys")
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
	operatorCommand := fmt.Sprintf("env PATH=%s%c%s%c%s SSHDOCK_DATA_DIR=%s SSHDOCK_COMPOSE_RUNNER=fake SSHDOCK_FAKE_COMPOSE_SERVICES=web:running SSHDOCK_FAKE_COMPOSE_LOGS=first-dashboard-log SSHDOCK_FAKE_COMPOSE_EXEC_OUTPUT=exec-output SSHDOCK_FAKE_COMPOSE_RUN_OUTPUT=run-output SSHDOCK_CADDY_CONFIG_PATH=%s %s operator",
		fakeBinDir,
		os.PathListSeparator,
		installBinDir,
		os.PathListSeparator,
		os.Getenv("PATH"),
		dataDir,
		filepath.Join(tmp, "operator.caddyfile"),
		filepath.Join(installBinDir, "sshdockd"),
	)
	cliEnv := append(os.Environ(),
		"PATH="+installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_GIT_AUTHORIZED_KEYS_PATH="+authorizedKeysPath,
		"SSHDOCK_GIT_RECEIVE_COMMAND="+receiveCommand,
		"SSHDOCK_OPERATOR_AUTHORIZED_KEYS_PATH="+operatorAuthorizedKeysPath,
		"SSHDOCK_OPERATOR_COMMAND="+operatorCommand,
		"SSHDOCK_COMPOSE_RUNNER="+composeRunner,
	)
	runCommandInput(t, root, cliEnv, publicKey, filepath.Join(installBinDir, "sshdock"), "ssh-keys", "add", "admin")

	daemonLogPath := filepath.Join(tmp, "sshdockd.log")
	daemonLog, err := os.Create(daemonLogPath)
	if err != nil {
		t.Fatalf("Create daemon log: %v", err)
	}
	daemonCtx, cancelDaemon := context.WithCancel(context.Background())
	daemon := exec.CommandContext(daemonCtx, filepath.Join(installBinDir, "sshdockd"), "daemon")
	daemon.Env = append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SSHDOCK_DATA_DIR="+dataDir,
		"SSHDOCK_COMPOSE_RUNNER="+composeRunner,
	)
	daemon.Stdout = daemonLog
	daemon.Stderr = daemonLog
	if err := daemon.Start(); err != nil {
		cancelDaemon()
		_ = daemonLog.Close()
		t.Fatalf("start sshdockd daemon: %v", err)
	}
	t.Cleanup(func() {
		cancelDaemon()
		_ = daemon.Wait()
		_ = daemonLog.Close()
	})

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
		tmp:                        tmp,
		installBinDir:              installBinDir,
		dataDir:                    dataDir,
		clientKeyPath:              clientKeyPath,
		operatorAuthorizedKeysPath: operatorAuthorizedKeysPath,
		sshPort:                    port,
		sshUser:                    currentUser.Username,
	}
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

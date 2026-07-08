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

type dashboardSSHServer struct {
	Port    int
	LogPath string
}

func startDashboardSSHServer(t *testing.T, paths serverPushPaths, sshdPath string, sshKeygenPath string) dashboardSSHServer {
	t.Helper()
	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("current user: %v", err)
	}
	if currentUser.Username == "" {
		t.Skip("current user name is required for OpenSSH dashboard e2e")
	}

	hostKeyPath := filepath.Join(paths.tmp, "dashboard_host_ed25519")
	runCommand(t, paths.tmp, nil, sshKeygenPath, "-t", "ed25519", "-N", "", "-f", hostKeyPath)
	port := freeLocalPort(t)
	sshdConfigPath := filepath.Join(paths.tmp, "dashboard_sshd_config")
	sshdLogPath := filepath.Join(paths.tmp, "dashboard_sshd.log")
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
`, port, hostKeyPath, filepath.Join(paths.tmp, "dashboard_sshd.pid"), paths.dashboardAuthorizedKeysPath, currentUser.Username)
	if err := os.WriteFile(sshdConfigPath, []byte(sshdConfig), 0o600); err != nil {
		t.Fatalf("WriteFile dashboard sshd_config: %v", err)
	}
	if output, err := exec.Command(sshdPath, "-t", "-f", sshdConfigPath).CombinedOutput(); err != nil {
		t.Skipf("OpenSSH dashboard config is not usable in this environment: %v\n%s", err, output)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sshd := exec.CommandContext(ctx, sshdPath, "-D", "-e", "-f", sshdConfigPath)
	logFile, err := os.Create(sshdLogPath)
	if err != nil {
		cancel()
		t.Fatalf("Create dashboard sshd log: %v", err)
	}
	sshd.Stdout = logFile
	sshd.Stderr = logFile
	if err := sshd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		t.Skipf("start dashboard sshd: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = sshd.Wait()
		_ = logFile.Close()
	})
	waitForTCP(t, "127.0.0.1", port, sshdLogPath)

	return dashboardSSHServer{Port: port, LogPath: sshdLogPath}
}

func dashboardSSHArgs(paths serverPushPaths, server dashboardSSHServer, tty bool) []string {
	args := []string{}
	if tty {
		args = append(args, "-tt")
	} else {
		args = append(args, "-T")
	}
	args = append(args,
		"-p", fmt.Sprintf("%d", server.Port),
		"-i", paths.clientKeyPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		paths.sshUser+"@127.0.0.1",
	)
	return args
}

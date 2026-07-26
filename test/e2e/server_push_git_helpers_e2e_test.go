//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func pushComposeAppThroughSSH(t *testing.T, paths serverPushPaths, appName string, files map[string]string) string {
	t.Helper()
	commitSHA, _ := pushComposeAppThroughSSHWithOutput(t, paths, appName, files)
	return commitSHA
}

func pushComposeAppThroughSSHWithOutput(t *testing.T, paths serverPushPaths, appName string, files map[string]string) (string, string) {
	t.Helper()
	push := prepareComposeAppPush(t, paths, composePushRequest{AppName: appName, Files: files})
	output := runGitOutput(t, push.sourceDir, push.env, "push", "sshdock", "main")
	return push.commitSHA, output
}

type composePushRequest struct {
	AppName string
	Files   map[string]string
}

type composePush struct {
	commitSHA string
	sourceDir string
	env       []string
}

func prepareComposeAppPush(t *testing.T, paths serverPushPaths, request composePushRequest) composePush {
	t.Helper()
	sourceDir := filepath.Join(paths.tmp, "source-"+request.AppName)
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source: %v", err)
	}
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	for name, content := range request.Files {
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
	runGit(t, sourceDir, nil, "remote", "add", "sshdock", paths.sshUser+"@127.0.0.1:"+request.AppName+".git")

	sshPath := requireCommandOrSkip(t, "ssh")
	sshCommand := fmt.Sprintf("%s -p %d -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", sshPath, paths.sshPort, paths.clientKeyPath)
	pushEnv := append(os.Environ(),
		"PATH="+paths.installBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_SSH_COMMAND="+sshCommand,
		"SSHDOCK_DATA_DIR="+paths.dataDir,
	)
	return composePush{commitSHA: commitSHA, sourceDir: sourceDir, env: pushEnv}
}

type pushCommandSignalWriter struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	want    string
	matched chan struct{}
	once    sync.Once
}

func newPushCommandSignalWriter(want string) *pushCommandSignalWriter {
	return &pushCommandSignalWriter{want: want, matched: make(chan struct{})}
}

func (w *pushCommandSignalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buffer.Write(p)
	matched := strings.Contains(w.buffer.String(), w.want)
	w.mu.Unlock()
	if matched {
		w.once.Do(func() { close(w.matched) })
	}
	return n, err
}

func (w *pushCommandSignalWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

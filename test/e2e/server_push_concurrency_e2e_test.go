//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/deploycoord"
)

func TestServerPushConcurrencyEndToEnd(t *testing.T) {
	// Given
	paths := setupBootstrappedServerPush(t, "fake")
	manager := deploycoord.NewManager(filepath.Join(paths.dataDir, "locks"))
	appGuard, err := manager.AcquireApp(context.Background(), "locked-app")
	if err != nil {
		t.Fatalf("AcquireApp: %v", err)
	}

	// When: another receive owns the same app lock.
	lockedOutput, lockedErr := pushFilesThroughBootstrappedSSH(t, paths, "locked-app", map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	})

	// Then: the push is rejected before receive-pack starts and succeeds after release.
	if lockedErr == nil || !strings.Contains(lockedOutput, `another push is already running for app "locked-app"`) {
		t.Fatalf("same-app push error = %v, output:\n%s", lockedErr, lockedOutput)
	}
	if err := appGuard.Release(); err != nil {
		t.Fatalf("release app lock: %v", err)
	}
	retryOutput, retryErr := pushFilesThroughBootstrappedSSH(t, paths, "locked-app", map[string]string{
		"compose.yml": "services:\n  web:\n    image: example/web:latest\n",
	})
	if retryErr != nil {
		t.Fatalf("same-app retry: %v\n%s", retryErr, retryOutput)
	}

	// When: another app deployment owns the server-wide slot.
	deploymentGuard, err := manager.AcquireDeployment(context.Background(), nil)
	if err != nil {
		t.Fatalf("AcquireDeployment: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command, output := prepareConcurrentPush(t, ctx, paths, "waiting-app")
	if err := command.Start(); err != nil {
		_ = deploymentGuard.Release()
		t.Fatalf("start waiting push: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-output.waiting:
	case err := <-done:
		_ = deploymentGuard.Release()
		t.Fatalf("waiting push exited before wait output: %v\n%s", err, output.String())
	case <-ctx.Done():
		_ = deploymentGuard.Release()
		t.Fatalf("waiting push did not report contention: %v\n%s", ctx.Err(), output.String())
	}
	select {
	case err := <-done:
		_ = deploymentGuard.Release()
		t.Fatalf("waiting push exited while deployment slot was held: %v\n%s", err, output.String())
	default:
	}
	if _, err := os.Stat(filepath.Join(paths.dataDir, "apps", "waiting-app", "repo.git")); !errors.Is(err, os.ErrNotExist) {
		_ = deploymentGuard.Release()
		t.Fatalf("waiting app repo stat error = %v, want receive-pack not started", err)
	}
	if err := deploymentGuard.Release(); err != nil {
		t.Fatalf("release deployment lock: %v", err)
	}

	// Then: the same Git connection resumes and completes its deployment.
	if err := <-done; err != nil {
		t.Fatalf("waiting push after release: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "deploy: waiting for another app deployment to finish") {
		t.Fatalf("push output missing wait guidance:\n%s", output.String())
	}
}

func prepareConcurrentPush(t *testing.T, ctx context.Context, paths serverPushPaths, appName string) (*exec.Cmd, *concurrentPushOutput) {
	t.Helper()
	sourceDir := t.TempDir()
	runGit(t, sourceDir, nil, "init")
	runGit(t, sourceDir, nil, "config", "user.email", "dev@example.com")
	runGit(t, sourceDir, nil, "config", "user.name", "SSHDock Test")
	runGit(t, sourceDir, nil, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(sourceDir, "compose.yml"), []byte("services:\n  web:\n    image: example/web:latest\n"), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	runGit(t, sourceDir, nil, "add", ".")
	runGit(t, sourceDir, nil, "commit", "-m", "exercise deployment wait")
	runGit(t, sourceDir, nil, "remote", "add", "sshdock", paths.sshUser+"@127.0.0.1:"+appName+".git")

	output := newConcurrentPushOutput("deploy: waiting for another app deployment to finish")
	command := exec.CommandContext(ctx, "git", "push", "sshdock", "main")
	command.Dir = sourceDir
	command.Env = currentMainPushEnv(t, paths)
	command.Stdout = output
	command.Stderr = output
	return command, output
}

type concurrentPushOutput struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	want    string
	waiting chan struct{}
	once    sync.Once
}

func newConcurrentPushOutput(want string) *concurrentPushOutput {
	return &concurrentPushOutput{want: want, waiting: make(chan struct{})}
}

func (o *concurrentPushOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	written, err := o.buffer.Write(data)
	if strings.Contains(o.buffer.String(), o.want) {
		o.once.Do(func() { close(o.waiting) })
	}
	return written, err
}

func (o *concurrentPushOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buffer.String()
}

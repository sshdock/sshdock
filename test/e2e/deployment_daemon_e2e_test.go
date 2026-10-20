//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func startDeploymentDaemon(t *testing.T, binary string, env []string) func() {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "sshdockd.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, binary, "daemon")
	command.Env, command.Stdout, command.Stderr = env, log, log
	if err := command.Start(); err != nil {
		cancel()
		_ = log.Close()
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			_ = command.Wait()
			_ = log.Close()
			if t.Failed() {
				t.Logf("deployment daemon:\n%s", readFile(t, logPath))
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

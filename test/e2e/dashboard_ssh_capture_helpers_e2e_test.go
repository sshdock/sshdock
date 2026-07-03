//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/iketiunn/rumbase/internal/tui/capture"
)

type dashboardCaptureOptions struct {
	SSHPath     string
	Paths       serverPushPaths
	Server      dashboardSSHServer
	AppName     string
	ArtifactDir string
	Rows        int
	Cols        int
	Timeout     time.Duration
}

func captureDashboardSSHSession(t *testing.T, options dashboardCaptureOptions) capture.Manifest {
	t.Helper()
	if options.Rows == 0 {
		options.Rows = 32
	}
	if options.Cols == 0 {
		options.Cols = 120
	}
	if options.Timeout == 0 {
		options.Timeout = 20 * time.Second
	}

	cmd := exec.Command(options.SSHPath, dashboardSSHArgs(options.Paths, options.Server, true)...)
	cmd.Dir = options.Paths.tmp
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(options.Rows),
		Cols: uint16(options.Cols),
	})
	if err != nil {
		t.Fatalf("start dashboard ssh PTY: %v", err)
	}
	defer func() { _ = ptmx.Close() }()

	terminal := capture.NewTerminal(options.Rows, options.Cols, ptmx)
	var mu sync.Mutex
	var raw bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		readDone <- replayPTYOutput(ptmx, terminal, &raw, &mu)
	}()

	specs := []dashboardFrameSpec{
		{Name: "summary", Wants: []string{"Rhumbase", options.AppName, "[Summary]"}},
		{Name: "services", Key: "\t", Wants: []string{"[Services]", "Service", "State", "web", "running"}},
		{Name: "routes", Key: "\t", Wants: []string{"[Routes]", "- none"}},
		{Name: "releases", Key: "\t", Wants: []string{"[Releases]", "Release", "succeeded"}},
		{Name: "deploys", Key: "\t", Wants: []string{"[Deploys]", "Deploy", "succeeded"}},
		{Name: "logs", Key: "\t", Wants: []string{"[Logs]", "first-dashboard-log"}},
	}
	frames := make([]capture.Frame, 0, len(specs))
	for _, spec := range specs {
		if spec.Key != "" {
			if _, err := io.WriteString(ptmx, spec.Key); err != nil {
				t.Fatalf("write key for %s: %v", spec.Name, err)
			}
		}
		screen := waitForDashboardScreen(t, terminal, &mu, options.Timeout, spec.Name, spec.Wants)
		frames = append(frames, capture.Frame{Name: spec.Name, Screen: screen})
	}

	if _, err := io.WriteString(ptmx, "q"); err != nil {
		t.Fatalf("write quit key: %v", err)
	}
	waitForDashboardSSHExit(t, cmd, ptmx)
	waitForPTYReplayDone(t, readDone)

	mu.Lock()
	rawBytes := append([]byte(nil), raw.Bytes()...)
	mu.Unlock()
	manifest, err := capture.WriteArtifacts(capture.ArtifactSet{
		Dir:     options.ArtifactDir,
		Raw:     rawBytes,
		Frames:  frames,
		Rows:    options.Rows,
		Cols:    options.Cols,
		Command: append([]string{options.SSHPath}, dashboardSSHArgs(options.Paths, options.Server, true)...),
	})
	if err != nil {
		t.Fatalf("write dashboard screenshot artifacts: %v", err)
	}
	return manifest
}

type dashboardFrameSpec struct {
	Name  string
	Key   string
	Wants []string
}

func replayPTYOutput(ptmx *os.File, terminal *capture.Terminal, raw *bytes.Buffer, mu *sync.Mutex) error {
	buffer := make([]byte, 4096)
	for {
		n, err := ptmx.Read(buffer)
		if n > 0 {
			chunk := append([]byte(nil), buffer[:n]...)
			mu.Lock()
			raw.Write(chunk)
			_, writeErr := terminal.Write(chunk)
			mu.Unlock()
			if writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			return err
		}
	}
}

func waitForDashboardScreen(t *testing.T, terminal *capture.Terminal, mu *sync.Mutex, timeout time.Duration, frameName string, wants []string) capture.Screen {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		mu.Lock()
		screen := terminal.Screen()
		text := screen.Text()
		mu.Unlock()
		if containsAll(text, wants) {
			return screen
		}
		last = text
		time.Sleep(75 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for dashboard frame %s containing %v\nlast screen:\n%s", frameName, wants, last)
	return capture.Screen{}
}

func containsAll(text string, wants []string) bool {
	for _, want := range wants {
		if !strings.Contains(text, want) {
			return false
		}
	}
	return true
}

func waitForDashboardSSHExit(t *testing.T, cmd *exec.Cmd, ptmx *os.File) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("dashboard ssh exited with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
		t.Fatal("dashboard ssh did not exit after quit")
	}
	_ = ptmx.Close()
}

func waitForPTYReplayDone(t *testing.T, readDone <-chan error) {
	t.Helper()
	select {
	case err := <-readDone:
		if err != nil && !isExpectedPTYCloseError(err) {
			t.Fatalf("read dashboard PTY output: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PTY replay reader")
	}
}

func isExpectedPTYCloseError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "input/output error") ||
		strings.Contains(message, "file already closed") ||
		strings.Contains(message, "bad file descriptor")
}

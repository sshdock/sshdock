package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestAppsStartAndStopCommandsUseLifecycleBackend(t *testing.T) {
	// Given
	backend := NewMemoryBackend("server")
	backend.apps["my-app"] = App{Name: "my-app", Status: "healthy", NodeID: "local"}
	runner := NewRunner(backend, "dev")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "start", args: []string{"apps", "start", "my-app"}, want: "started app my-app\n"},
		{name: "stop", args: []string{"apps", "stop", "my-app"}, want: "stopped app my-app\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			// When
			code := runner.Run(test.args, &stdout, &stderr)

			// Then
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if stdout.String() != test.want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), test.want)
			}
		})
	}
}

func TestAppsStartMissingAppReturnsActionableError(t *testing.T) {
	// Given
	runner := NewRunner(NewMemoryBackend("server"), "dev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// When
	code := runner.Run([]string{"apps", "start", "missing"}, &stdout, &stderr)

	// Then
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), `app "missing" not found`) {
		t.Fatalf("stderr = %q, want missing app error", stderr.String())
	}
}

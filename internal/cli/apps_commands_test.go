package cli

import (
	"bytes"
	"io"
	"reflect"
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

func TestAppsExecAndRunRequireDelimiterAndPreserveCommandArgv(t *testing.T) {
	// Given
	backend := &recordingServiceCommandBackend{MemoryBackend: NewMemoryBackend("server")}
	backend.apps["my-app"] = App{Name: "my-app", Status: "healthy", NodeID: "local"}
	runner := NewRunner(backend, "dev").WithInteractiveTerminal(true)
	stdin := strings.NewReader("input\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := []string{"sh", "-c", `printf '%s\n' "$1"`, "_", "value with spaces"}

	// When
	execArgs := append([]string{"apps", "exec", "my-app", "web", "--"}, command...)
	if code := runner.RunWithInput(execArgs, stdin, &stdout, &stderr); code != 0 {
		t.Fatalf("apps exec exit code = %d, stderr = %q", code, stderr.String())
	}
	runArgs := append([]string{"apps", "run", "my-app", "worker", "--"}, command...)
	if code := runner.RunWithInput(runArgs, stdin, &stdout, &stderr); code != 0 {
		t.Fatalf("apps run exit code = %d, stderr = %q", code, stderr.String())
	}

	// Then
	if len(backend.execRequests) != 1 || len(backend.runRequests) != 1 {
		t.Fatalf("exec requests = %#v, run requests = %#v", backend.execRequests, backend.runRequests)
	}
	for _, request := range []ServiceCommandRequest{backend.execRequests[0], backend.runRequests[0]} {
		if request.AppName != "my-app" || !reflect.DeepEqual(request.Command, command) || !request.Interactive || request.Stdin != stdin || request.Stdout != &stdout || request.Stderr != &stderr {
			t.Fatalf("request = %#v, want preserved command and streams", request)
		}
	}
	if backend.execRequests[0].ServiceName != "web" || backend.runRequests[0].ServiceName != "worker" {
		t.Fatalf("service names were not preserved: exec=%q run=%q", backend.execRequests[0].ServiceName, backend.runRequests[0].ServiceName)
	}
	if stdout.String() != "exec output\nrun output\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}

	for _, args := range [][]string{
		{"apps", "exec", "my-app", "web", "printf", "missing delimiter"},
		{"apps", "run", "my-app", "web", "--"},
		{"apps", "exec", "my-app", "web", "-T", "--", "sh"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := runner.RunWithInput(args, stdin, &stdout, &stderr); code != 2 {
			t.Fatalf("%v exit code = %d, want 2", args, code)
		}
	}
}

type recordingServiceCommandBackend struct {
	*MemoryBackend
	execRequests []ServiceCommandRequest
	runRequests  []ServiceCommandRequest
}

func (b *recordingServiceCommandBackend) ExecApp(request ServiceCommandRequest) error {
	b.execRequests = append(b.execRequests, request)
	_, err := io.WriteString(request.Stdout, "exec output\n")
	return err
}

func (b *recordingServiceCommandBackend) RunApp(request ServiceCommandRequest) error {
	b.runRequests = append(b.runRequests, request)
	_, err := io.WriteString(request.Stdout, "run output\n")
	return err
}

package compose

import (
	"context"
	"strings"
	"testing"
)

func TestLocalCommandExecutorKeepsSuccessfulStderrOutOfStructuredOutput(t *testing.T) {
	// Given
	command := Command{Name: "sh", Args: []string{"-c", `printf '{"services":{}}'; printf 'compose warning' >&2`}}

	// When
	output, err := (LocalCommandExecutor{}).Run(context.Background(), command)

	// Then
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if output != `{"services":{}}` {
		t.Fatalf("output = %q, want structured stdout only", output)
	}
}

func TestLocalCommandExecutorIncludesStderrInCommandFailure(t *testing.T) {
	// Given
	command := Command{Name: "sh", Args: []string{"-c", `printf 'pull denied' >&2; exit 9`}}

	// When
	_, err := (LocalCommandExecutor{}).Run(context.Background(), command)

	// Then
	if err == nil || !strings.Contains(err.Error(), "pull denied") {
		t.Fatalf("Run error = %v, want stderr detail", err)
	}
}

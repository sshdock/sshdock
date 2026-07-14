package gitrecv

import (
	"strings"
	"testing"
)

func TestValidatePreReceiveAcceptsUpdateToMain(t *testing.T) {
	// Given
	input := strings.NewReader("oldsha newsha refs/heads/main\n")

	// When
	err := ValidatePreReceive(input)

	// Then
	if err != nil {
		t.Fatalf("ValidatePreReceive: %v", err)
	}
}

func TestValidatePreReceiveRejectsNonMainDestination(t *testing.T) {
	tests := []struct {
		name string
		ref  string
	}{
		{name: "branch", ref: "refs/heads/feature"},
		{name: "tag", ref: "refs/tags/v1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			input := strings.NewReader("oldsha newsha " + tt.ref + "\n")

			// When
			err := ValidatePreReceive(input)

			// Then
			if err == nil {
				t.Fatal("ValidatePreReceive error = nil, want rejection")
			}
			if !strings.Contains(err.Error(), "push to remote main") {
				t.Fatalf("ValidatePreReceive error = %q, want remote-main guidance", err)
			}
		})
	}
}

func TestValidatePreReceiveRejectsDeletingMain(t *testing.T) {
	// Given
	input := strings.NewReader("oldsha 0000000000000000000000000000000000000000 refs/heads/main\n")

	// When
	err := ValidatePreReceive(input)

	// Then
	if err == nil {
		t.Fatal("ValidatePreReceive error = nil, want rejection")
	}
	if !strings.Contains(err.Error(), "cannot delete remote main") {
		t.Fatalf("ValidatePreReceive error = %q, want deletion guidance", err)
	}
}

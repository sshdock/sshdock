package harness

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSmokeVersionCommands(t *testing.T) {
	root := filepath.Join("..", "..")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "rhumbase version",
			args: []string{"run", "./cmd/rhumbase", "version"},
			want: "rhumbase dev\n",
		},
		{
			name: "rhumbased version",
			args: []string{"run", "./cmd/rhumbased", "version"},
			want: "rhumbased dev\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("go", tt.args...)
			cmd.Dir = root

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", tt.name, err, output)
			}

			if string(output) != tt.want {
				t.Fatalf("output = %q, want %q", output, tt.want)
			}
		})
	}
}

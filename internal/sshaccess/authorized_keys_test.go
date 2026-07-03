package sshaccess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderAuthorizedKeysRestrictsDeployKeys(t *testing.T) {
	keys := []Key{
		{
			Name:      "admin",
			PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com",
			CreatedAt: time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC),
		},
	}

	rendered := RenderAuthorizedKeys(keys, "/usr/local/bin/rhumbased git-receive")

	for _, want := range []string{
		`command="exec /usr/local/bin/rhumbased git-receive"`,
		`no-pty`,
		`no-port-forwarding`,
		`no-agent-forwarding`,
		`no-X11-forwarding`,
		`no-user-rc`,
		`ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com`,
		`rhumbase-key:admin`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("authorized_keys missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderDashboardAuthorizedKeysAllowsPTYButKeepsForwardingRestrictions(t *testing.T) {
	keys := []Key{
		{
			Name:      "admin",
			PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com",
			CreatedAt: time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC),
		},
	}

	rendered := RenderDashboardAuthorizedKeys(keys, "/usr/local/bin/rhumbased dashboard")

	for _, want := range []string{
		`command="exec /usr/local/bin/rhumbased dashboard"`,
		`no-port-forwarding`,
		`no-agent-forwarding`,
		`no-X11-forwarding`,
		`no-user-rc`,
		`ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com`,
		`rhumbase-key:admin`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("dashboard authorized_keys missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "no-pty") {
		t.Fatalf("dashboard authorized_keys should allow PTY:\n%s", rendered)
	}
}

func TestWriteAuthorizedKeysCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git", ".ssh", "authorized_keys")
	keys := []Key{{Name: "admin", PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey"}}

	if err := WriteAuthorizedKeys(path, keys, "rhumbased git-receive"); err != nil {
		t.Fatalf("WriteAuthorizedKeys: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat authorized_keys: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("authorized_keys mode = %v, want 0600", info.Mode().Perm())
	}
}

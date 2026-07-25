package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBackupRestoreAndVolumeBoundaryFeatureLab_contract_when_reusing_wordpress_recipe(t *testing.T) {
	// Given the registered WordPress recipe and its backup-restore lab.
	root := repoRoot(t)
	labDir := filepath.Join(root, "examples", "labs", "backup-restore-and-volume-boundary")

	// When the lab's public interface is inspected.
	entries, err := os.ReadDir(labDir)
	if err != nil {
		t.Fatalf("ReadDir feature lab: %v", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("feature lab contains nested directory %q", entry.Name())
		}
		files = append(files, entry.Name())
	}
	slices.Sort(files)
	if want := []string{"README.md", "acceptance.sh"}; !slices.Equal(files, want) {
		t.Fatalf("feature lab files = %#v, want %#v", files, want)
	}

	readme := readTextFile(t, filepath.Join(labDir, "README.md"))
	for _, want := range []string{
		"examples/software/wordpress",
		"sudo sshdock apps create backup-restore-and-volume-boundary",
		"ssh sshdock@sshdock.example.com config set backup-restore-and-volume-boundary BACKUP_LAB_SECRET",
		"sudo sshdock backup create --output",
		"sudo sshdock backup inspect",
		"sudo systemctl stop sshdockd",
		"sudo sshdock backup restore",
		"sudo sshdock diagnostics",
		"sudo systemctl start sshdockd",
		"config get backup-restore-and-volume-boundary BACKUP_LAB_SECRET",
		"docker volume inspect sshdock_backup-restore-and-volume-boundary_wordpress-data",
		"Docker volume contents are not copied",
		"system `sshd` host key",
		"bash acceptance.sh",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README missing workflow marker %q", want)
		}
	}
	createIndex := strings.Index(readme, "sudo sshdock apps create backup-restore-and-volume-boundary")
	configIndex := strings.Index(readme, "config set backup-restore-and-volume-boundary WORDPRESS_DB_NAME")
	pushIndex := strings.Index(readme, "git push sshdock main")
	if createIndex < 0 || configIndex <= createIndex || pushIndex <= configIndex {
		t.Fatal("README does not create the app and set required config before the first Git push")
	}

	scriptPath := filepath.Join(labDir, "acceptance.sh")
	if output, err := exec.Command("bash", "-n", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("acceptance script syntax: %v\n%s", err, output)
	}
	script := readTextFile(t, scriptPath)
	if strings.Contains(script, "data/ssh_host_rsa_key") {
		t.Fatal("acceptance script treats the host SSH key as an SSHDock backup artifact")
	}
	for _, want := range []string{
		"APP=${SSHDOCK_APP:-backup-restore-and-volume-boundary}",
		"BACKUP_LAB_SECRET",
		"backup create --output",
		"backup inspect",
		"data/git/.ssh/authorized_keys",
		"data/.ssh/authorized_keys",
		"systemctl stop sshdockd",
		"backup restore",
		"sshdock diagnostics",
		"systemctl start sshdockd",
		"config get \"$APP\" BACKUP_LAB_SECRET",
		"apps health \"$APP\"",
		"domains list \"$APP\"",
		"docker volume inspect",
		"--include-volumes",
		"not implemented",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("acceptance script missing %q", want)
		}
	}

	guide := readTextFile(t, filepath.Join(root, "docs", "EXAMPLES.md"))
	if !strings.Contains(guide, "examples/labs/backup-restore-and-volume-boundary") {
		t.Fatal("public examples guide does not register the backup-restore-and-volume-boundary feature lab")
	}
}

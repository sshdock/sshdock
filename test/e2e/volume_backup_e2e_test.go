//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/backup"
)

func TestDockerVolumeBackupRestoresFreshVolumeEndToEnd(t *testing.T) {
	if os.Getenv("SSHDOCK_E2E_DOCKER") != "1" {
		t.Skip("set SSHDOCK_E2E_DOCKER=1 to run Docker volume backup tests")
	}
	requireDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	prefix := fmt.Sprintf("volume-backup-%d", time.Now().UnixNano())
	source, target := prefix+"-source", prefix+"-restored"
	writer, probe := prefix+"-foreign-writer", prefix+"-probe"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, name := range []string{writer, probe} {
			_ = exec.CommandContext(cleanupCtx, "docker", "container", "rm", "--force", "--", name).Run()
		}
		for _, name := range []string{source, target} {
			_ = exec.CommandContext(cleanupCtx, "docker", "volume", "rm", "--", name).Run()
		}
	})
	volumeBackupDocker(t, ctx, nil, "pull", lifecycleBusyboxImage)
	volumeBackupDocker(t, ctx, nil, "volume", "create", "--driver", "local", "--", source)
	binary := make([]byte, 4096)
	for i := range binary {
		binary[i] = byte(i)
	}
	plain := "volume backup bytes\n"
	seed := `set -eu
mkdir -p /data/nested /data/empty
printf 'volume backup bytes\n' > /data/plain.txt
cat > /data/nested/binary.dat
chown 12001:12002 /data /data/plain.txt
chown 22001:22002 /data/nested /data/nested/binary.dat
chown 32001:32002 /data/empty
chmod 0750 /data
chmod 0640 /data/plain.txt
chmod 0710 /data/nested
chmod 0600 /data/nested/binary.dat
chmod 0000 /data/empty`
	volumeBackupDocker(t, ctx, binary, "run", "--rm", "--interactive", "--name", probe, "--network", "none",
		"--mount", "type=volume,src="+source+",dst=/data,volume-nocopy", lifecycleBusyboxImage, "sh", "-c", seed)

	// A foreign Compose project is still a writer even when the source app is idle.
	volumeBackupDocker(t, ctx, nil, "run", "--detach", "--name", writer, "--network", "none",
		"--label", "com.docker.compose.project="+prefix+"-foreign",
		"--mount", "type=volume,src="+source+",dst=/data,volume-nocopy", lifecycleBusyboxImage, "sleep", "300")
	archive := filepath.Join(t.TempDir(), "volume.tar.gz")
	request := backup.VolumeExportRequest{VolumeName: source, ArchivePath: archive, Quiesced: true}
	if _, err := backup.ExportVolume(ctx, request); err == nil || !strings.Contains(err.Error(), "running read/write") {
		t.Fatalf("export with foreign writer error = %v", err)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("rejected export published archive: %v", err)
	}
	volumeBackupDocker(t, ctx, nil, "container", "rm", "--force", "--", writer)

	exported, err := backup.ExportVolume(ctx, request)
	if err != nil {
		t.Fatalf("ExportVolume: %v", err)
	}
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatalf("stat exported archive: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode = %o, want 600", info.Mode().Perm())
	}
	inspected, err := backup.InspectVolume(ctx, archive)
	if err != nil {
		t.Fatalf("InspectVolume: %v", err)
	}
	if inspected != exported || inspected.Manifest.FormatVersion != backup.VolumeFormatVersion ||
		inspected.Manifest.SourceVolume != source || inspected.Manifest.Driver != "local" || inspected.Manifest.CreatedAt.IsZero() ||
		inspected.FileCount != 2 || inspected.DirectoryCount != 3 || inspected.TotalBytes != int64(len(binary)+len(plain)) {
		t.Fatalf("unexpected archive content profile: %#v; export %#v", inspected, exported)
	}

	volumeBackupDocker(t, ctx, nil, "volume", "rm", "--", source)
	remaining := volumeBackupDocker(t, ctx, nil, "volume", "ls", "--format", "{{.Name}}")
	for _, name := range strings.Fields(string(remaining)) {
		if name == source {
			t.Fatal("source volume still exists before restore")
		}
	}
	restore := backup.VolumeRestoreRequest{VolumeName: target, ArchivePath: archive}
	restored, err := backup.RestoreVolume(ctx, restore)
	if err != nil {
		t.Fatalf("RestoreVolume into fresh target: %v", err)
	}
	if restored != inspected {
		t.Fatalf("restore inspection = %#v, want %#v", restored, inspected)
	}
	verify := func(wantPlain string) {
		t.Helper()
		args := []string{"run", "--rm", "--name", probe, "--network", "none", "--mount",
			"type=volume,src=" + target + ",dst=/data,volume-nocopy,readonly", lifecycleBusyboxImage}
		metadata := volumeBackupDocker(t, ctx, nil, append(args, "sh", "-c",
			"cd /data && stat -c '%n|%u|%g|%a' . plain.txt nested nested/binary.dat empty && test -d empty && test -z \"$(ls -A empty)\"")...)
		wantMetadata := ".|12001|12002|750\nplain.txt|12001|12002|640\nnested|22001|22002|710\nnested/binary.dat|22001|22002|600\nempty|32001|32002|0\n"
		if string(metadata) != wantMetadata {
			t.Fatalf("restored metadata = %q, want %q", metadata, wantMetadata)
		}
		if got := volumeBackupDocker(t, ctx, nil, append(args, "cat", "/data/nested/binary.dat")...); !bytes.Equal(got, binary) {
			t.Fatalf("restored binary differs: got %d bytes, want %d", len(got), len(binary))
		}
		if got := volumeBackupDocker(t, ctx, nil, append(args, "cat", "/data/plain.txt")...); string(got) != wantPlain {
			t.Fatalf("restored plain file = %q, want %q", got, wantPlain)
		}
	}
	verify(plain)

	// Change restored data so a mistaken second extraction cannot pass unnoticed.
	changed := "written after restore\n"
	volumeBackupDocker(t, ctx, []byte(changed), "run", "--rm", "--interactive", "--name", probe, "--network", "none",
		"--mount", "type=volume,src="+target+",dst=/data,volume-nocopy", lifecycleBusyboxImage,
		"sh", "-c", "cat > /data/plain.txt")
	if _, err := backup.RestoreVolume(ctx, restore); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("restore into existing target error = %v", err)
	}
	verify(changed)
}

func volumeBackupDocker(t *testing.T, ctx context.Context, input []byte, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes()
}

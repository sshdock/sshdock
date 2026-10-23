package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

func TestExportVolumeStreamsPrivateArchiveWithRestrictedHelper(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	executor := newVolumeTestExecutor(t)
	archive := filepath.Join(t.TempDir(), "nested", "volume.tar.gz")
	inspection, err := ExportVolume(context.Background(), VolumeExportRequest{VolumeName: "source_data", ArchivePath: archive, Quiesced: true, Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Path != archive || inspection.Manifest.SourceVolume != "source_data" || inspection.Manifest.Driver != "local" || inspection.FileCount != 1 {
		t.Fatalf("unexpected inspection: %#v", inspection)
	}
	info, err := os.Stat(archive)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("archive permissions: %v, %v", info, err)
	}
	helper := executor.helperCommand(t)
	for _, want := range []string{volumeHelperImage, "--pull=never", "--read-only", "no-new-privileges:true", "DAC_READ_SEARCH", "--format=pax", "--acls", "--xattrs", "--xattrs-include=*", "--selinux", "--sparse", "type=volume,src=source_data,dst=/data,volume-nocopy,readonly"} {
		if !containsVolumeArg(helper.Args, want) {
			t.Fatalf("helper missing %q: %#v", want, helper.Args)
		}
	}
	if volumeArgAfter(helper.Args, "--network") != "none" || volumeArgAfter(helper.Args, "--cap-drop") != "ALL" || containsVolumeArg(helper.Args, "SYS_ADMIN") || containsVolumeArg(helper.Args, "--privileged") {
		t.Fatalf("unsafe helper: %#v", helper.Args)
	}
	if executor.consumerChecks != 3 || executor.removedVolumes != 0 {
		t.Fatalf("consumer checks=%d, removed volumes=%d", executor.consumerChecks, executor.removedVolumes)
	}
	assertVolumeTemporaryEmpty(t, temporary)
}

func TestVolumeOperationsRejectInvalidArgumentsWithoutDocker(t *testing.T) {
	for _, name := range []string{"", "../data", "--help", "data,readonly", "data volume", "/host/path", "data\nother", strings.Repeat("v", 256)} {
		t.Run(name, func(t *testing.T) {
			executor := newVolumeTestExecutor(t)
			_, exportErr := ExportVolume(context.Background(), VolumeExportRequest{VolumeName: name, ArchivePath: "out.tar.gz", Quiesced: true, Executor: executor})
			_, restoreErr := RestoreVolume(context.Background(), VolumeRestoreRequest{VolumeName: name, ArchivePath: "in.tar.gz", Executor: executor})
			if exportErr == nil || restoreErr == nil || len(executor.commands) != 0 {
				t.Fatalf("invalid arguments reached Docker: export=%v restore=%v commands=%d", exportErr, restoreErr, len(executor.commands))
			}
		})
	}
	executor := newVolumeTestExecutor(t)
	if _, err := ExportVolume(context.Background(), VolumeExportRequest{VolumeName: "source_data", ArchivePath: "out.tar.gz", Executor: executor}); err == nil || !strings.Contains(err.Error(), "--quiesced") || len(executor.commands) != 0 {
		t.Fatalf("missing quiescence acknowledgement: %v", err)
	}
}

func TestExportVolumeRejectsCustomStorageBeforeTransfer(t *testing.T) {
	for _, source := range []dockerVolumeMetadata{
		{Name: "source_data", Driver: "nfs", Scope: "local"},
		{Name: "source_data", Driver: "local", Scope: "global"},
		{Name: "source_data", Driver: "local", Scope: "local", Options: map[string]string{"type": "none", "device": "/host/private", "o": "bind"}},
	} {
		t.Run(source.Driver+source.Scope+source.Options["type"], func(t *testing.T) {
			executor := newVolumeTestExecutor(t)
			executor.volumes["source_data"] = source
			_, err := ExportVolume(context.Background(), VolumeExportRequest{VolumeName: "source_data", ArchivePath: filepath.Join(t.TempDir(), "archive"), Quiesced: true, Executor: executor})
			if err == nil || !strings.Contains(err.Error(), "ordinary local volume") || len(executor.commands) != 1 {
				t.Fatalf("custom storage reached transfer: %v, commands=%#v", err, executor.commands)
			}
		})
	}
}

func TestExportVolumeObservesAllRunningWritableConsumers(t *testing.T) {
	for _, test := range []struct {
		name, mountName string
		running, rw     bool
		blocked         bool
	}{
		{"running writer from another app", "source_data", true, true, true},
		{"stopped writer", "source_data", false, true, false},
		{"running reader", "source_data", true, false, false},
		{"different exact volume", "source_data_other", true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := newVolumeTestExecutor(t)
			executor.consumers = []volumeTestConsumer{{ID: volumeTestWriterID, Running: test.running, Mounts: []volumeTestMount{{Type: "volume", Name: test.mountName, RW: test.rw}}}}
			_, err := ExportVolume(context.Background(), VolumeExportRequest{VolumeName: "source_data", ArchivePath: filepath.Join(t.TempDir(), "archive"), Quiesced: true, Executor: executor})
			if (err != nil) != test.blocked {
				t.Fatalf("export error=%v, blocked=%t", err, test.blocked)
			}
			if test.blocked && executor.transferred {
				t.Fatal("export copied data from running writer")
			}
		})
	}
}

func TestExportVolumeRejectsWriterAppearingDuringPullOrCopy(t *testing.T) {
	for _, phase := range []string{"pull", "copy"} {
		t.Run(phase, func(t *testing.T) {
			executor := newVolumeTestExecutor(t)
			addWriter := func() {
				executor.consumers = []volumeTestConsumer{{ID: volumeTestWriterID, Running: true, Mounts: []volumeTestMount{{Type: "volume", Name: "source_data", RW: true}}}}
			}
			if phase == "pull" {
				executor.afterPull = addWriter
			} else {
				executor.afterHelper = addWriter
			}
			archive := filepath.Join(t.TempDir(), "archive")
			_, err := ExportVolume(context.Background(), VolumeExportRequest{VolumeName: "source_data", ArchivePath: archive, Quiesced: true, Executor: executor})
			if err == nil || !strings.Contains(err.Error(), "running read/write") {
				t.Fatalf("late writer was accepted: %v", err)
			}
			if _, err := os.Stat(archive); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("late writer published archive: %v", err)
			}
		})
	}
}

func TestRestoreVolumeValidatesBeforeDockerAndRefusesExistingTarget(t *testing.T) {
	t.Run("invalid archive", func(t *testing.T) {
		executor := newVolumeTestExecutor(t)
		archive := filepath.Join(t.TempDir(), "invalid")
		if err := os.WriteFile(archive, []byte("not an archive"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := RestoreVolume(context.Background(), VolumeRestoreRequest{VolumeName: "new_data", ArchivePath: archive, Executor: executor}); err == nil || len(executor.commands) != 0 {
			t.Fatalf("invalid archive reached Docker: %v, %#v", err, executor.commands)
		}
	})
	t.Run("existing target", func(t *testing.T) {
		executor := newVolumeTestExecutor(t)
		archive := volumeOrchestrationArchive(t, executor.nativeTar)
		_, err := RestoreVolume(context.Background(), VolumeRestoreRequest{VolumeName: "source_data", ArchivePath: archive, Executor: executor})
		if err == nil || !strings.Contains(err.Error(), "already exists") || len(executor.commands) != 1 || executor.removedVolumes != 0 {
			t.Fatalf("existing volume was changed: %v, %#v", err, executor.commands)
		}
	})
}

func TestRestoreVolumeStreamsIntoVerifiedFreshVolume(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	executor := newVolumeTestExecutor(t)
	archive := volumeOrchestrationArchive(t, executor.nativeTar)
	inspection, err := RestoreVolume(context.Background(), VolumeRestoreRequest{VolumeName: "new_data", ArchivePath: archive, Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Manifest.SourceVolume != "source_data" || executor.volumes["new_data"].Operation == "" || executor.removedVolumes != 0 {
		t.Fatalf("fresh restore identity=%#v volumes=%#v", inspection, executor.volumes)
	}
	helper := executor.helperCommand(t)
	for _, want := range []string{"--interactive", "CHOWN", "DAC_OVERRIDE", "FOWNER", "--same-owner", "--same-permissions", "--delay-directory-restore", "type=volume,src=new_data,dst=/data,volume-nocopy"} {
		if !containsVolumeArg(helper.Args, want) {
			t.Fatalf("restore helper missing %q: %#v", want, helper.Args)
		}
	}
	reader := tar.NewReader(bytes.NewReader(executor.restoredTar))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			t.Fatal("restore tar missing data file")
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimPrefix(header.Name, "./") == "state.bin" {
			data, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(data, volumeTestPayload) || header.Uid != 1001 || header.Gid != 1002 || header.Mode != 0o640 {
				t.Fatalf("restore changed binary data or filesystem metadata: header=%#v, error=%v", header, err)
			}
			break
		}
	}
	assertVolumeTemporaryEmpty(t, temporary)
}

func TestRestoreVolumeDoesNotTouchVolumeCreatedByRacingOperation(t *testing.T) {
	executor := newVolumeTestExecutor(t)
	executor.raceCreate = true
	archive := volumeOrchestrationArchive(t, executor.nativeTar)
	_, err := RestoreVolume(context.Background(), VolumeRestoreRequest{VolumeName: "new_data", ArchivePath: archive, Executor: executor})
	if err == nil || !strings.Contains(err.Error(), "another operation") || executor.transferred || executor.removedVolumes != 0 || executor.volumes["new_data"].Operation != "another-operation" {
		t.Fatalf("racing volume changed: %v, %#v", err, executor.volumes)
	}
}

func TestRestoreVolumeCancellationCleansOwnedHelperAndVolumeWithFreshContext(t *testing.T) {
	for _, phase := range []string{"create", "transfer"} {
		t.Run(phase, func(t *testing.T) {
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			executor := newVolumeTestExecutor(t)
			archive := volumeOrchestrationArchive(t, executor.nativeTar)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "create" {
				executor.afterCreate = func() error { cancel(); return context.Canceled }
			} else {
				executor.helperFailure = func() error { cancel(); return context.Canceled }
			}
			_, err := RestoreVolume(ctx, VolumeRestoreRequest{VolumeName: "new_data", ArchivePath: archive, Executor: executor})
			if !errors.Is(err, context.Canceled) || executor.volumes["new_data"].Name != "" || executor.helperRemaining || executor.removedVolumes != 1 {
				t.Fatalf("cancelled operation leaked resources: error=%v volumes=%#v helper=%t", err, executor.volumes, executor.helperRemaining)
			}
			if phase == "transfer" && executor.removedHelpers != 1 {
				t.Fatal("cancelled Docker CLI left its helper behind")
			}
			assertVolumeTemporaryEmpty(t, temporary)
		})
	}
}

func TestVolumeHelperDiagnosticsAndFailuresNeverExposePayload(t *testing.T) {
	for _, restore := range []bool{false, true} {
		for _, failure := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{true: "restore", false: "export"}[restore], map[bool]string{true: "failure", false: "warning"}[failure]}, "/"), func(t *testing.T) {
				executor := newVolumeTestExecutor(t)
				secret := "private-payload\x00must-not-appear"
				if failure {
					executor.helperFailure = func() error { return errors.New(secret) }
				} else {
					executor.helperDiagnostics = secret
				}
				var err error
				if restore {
					_, err = RestoreVolume(context.Background(), VolumeRestoreRequest{VolumeName: "new_data", ArchivePath: volumeOrchestrationArchive(t, executor.nativeTar), Executor: executor})
				} else {
					_, err = ExportVolume(context.Background(), VolumeExportRequest{VolumeName: "source_data", ArchivePath: filepath.Join(t.TempDir(), "archive"), Quiesced: true, Executor: executor})
				}
				if err == nil || strings.Contains(err.Error(), secret) || executor.helperRemaining {
					t.Fatalf("unsafe helper failure: %v", err)
				}
				if restore && executor.volumes["new_data"].Name != "" {
					t.Fatal("failed restore retained incomplete owned volume")
				}
			})
		}
	}
}

func TestVolumeMetadataBoundAndCleanupFailure(t *testing.T) {
	t.Run("bounded metadata", func(t *testing.T) {
		executor := newVolumeTestExecutor(t)
		executor.metadataOverflow = true
		_, err := ExportVolume(context.Background(), VolumeExportRequest{VolumeName: "source_data", ArchivePath: filepath.Join(t.TempDir(), "archive"), Quiesced: true, Executor: executor})
		if err == nil || !strings.Contains(err.Error(), "more than") || executor.transferred {
			t.Fatalf("unbounded metadata accepted: %v", err)
		}
	})
	t.Run("actionable incomplete cleanup", func(t *testing.T) {
		executor := newVolumeTestExecutor(t)
		executor.helperFailure = func() error { return errors.New("private failure") }
		executor.cleanupFailure = true
		_, err := RestoreVolume(context.Background(), VolumeRestoreRequest{VolumeName: "new_data", ArchivePath: volumeOrchestrationArchive(t, executor.nativeTar), Executor: executor})
		if err == nil || !strings.Contains(err.Error(), "cleanup incomplete") || !strings.Contains(err.Error(), "new_data") || strings.Contains(err.Error(), "private failure") {
			t.Fatalf("cleanup guidance=%v", err)
		}
	})
}

const volumeTestWriterID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const volumeTestHelperID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

var volumeTestPayload = []byte("private\x00binary\xffdata\n")

type volumeTestMount struct {
	Type, Name string
	RW         bool
}
type volumeTestConsumer struct {
	ID      string            `json:"id"`
	Running bool              `json:"running"`
	Mounts  []volumeTestMount `json:"mounts"`
}
type volumeTestExecutor struct {
	t                                                                          *testing.T
	commands                                                                   []compose.Command
	volumes                                                                    map[string]dockerVolumeMetadata
	consumers                                                                  []volumeTestConsumer
	nativeTar, restoredTar                                                     []byte
	consumerChecks, removedHelpers, removedVolumes                             int
	transferred, helperRemaining, raceCreate, cleanupFailure, metadataOverflow bool
	helperDiagnostics                                                          string
	afterPull, afterHelper                                                     func()
	afterCreate, helperFailure                                                 func() error
}

func newVolumeTestExecutor(t *testing.T) *volumeTestExecutor {
	t.Helper()
	var raw bytes.Buffer
	writer := tar.NewWriter(&raw)
	for _, header := range []*tar.Header{{Name: "./", Typeflag: tar.TypeDir, Mode: 0o750, Uid: 1001, Gid: 1002}, {Name: "./state.bin", Typeflag: tar.TypeReg, Mode: 0o640, Uid: 1001, Gid: 1002, Size: int64(len(volumeTestPayload))}} {
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := writer.Write(volumeTestPayload); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &volumeTestExecutor{t: t, volumes: map[string]dockerVolumeMetadata{"source_data": {Name: "source_data", Driver: "local", Scope: "local"}}, nativeTar: raw.Bytes()}
}

func (executor *volumeTestExecutor) RunAttached(ctx context.Context, command compose.Command, stdin io.Reader, stdout, stderr io.Writer) error {
	executor.commands = append(executor.commands, command)
	args := command.Args
	if command.Name != "docker" {
		executor.t.Fatalf("unexpected command: %#v", command)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if args[0] == "pull" {
		if executor.afterPull != nil {
			executor.afterPull()
		}
		_, _ = io.WriteString(stderr, "image pull progress\n")
		return nil
	}
	if args[0] == "volume" {
		name := args[len(args)-1]
		switch args[1] {
		case "ls":
			var names []string
			for name := range executor.volumes {
				names = append(names, name)
			}
			sort.Strings(names)
			_, err := io.WriteString(stdout, strings.Join(names, "\n"))
			return err
		case "inspect":
			if executor.metadataOverflow {
				_, err := io.WriteString(stdout, strings.Repeat("private", volumeMetadataLimit/7+1))
				return err
			}
			volume, ok := executor.volumes[name]
			if !ok {
				return errors.New("missing volume")
			}
			return json.NewEncoder(stdout).Encode(volume)
		case "create":
			if _, exists := executor.volumes[name]; !exists {
				token := strings.TrimPrefix(volumeArgAfter(args, "--label"), volumeOperationLabel+"=")
				if executor.raceCreate {
					token = "another-operation"
				}
				executor.volumes[name] = dockerVolumeMetadata{Name: name, Driver: "local", Scope: "local", Operation: token}
			}
			if executor.afterCreate != nil {
				return executor.afterCreate()
			}
			return nil
		case "rm":
			if executor.helperRemaining {
				return errors.New("volume is in use")
			}
			delete(executor.volumes, name)
			executor.removedVolumes++
			return nil
		}
	}
	if args[0] == "container" {
		switch args[1] {
		case "ls":
			if strings.HasPrefix(volumeArgAfter(args, "--filter"), "name=") {
				if _, ok := ctx.Deadline(); !ok {
					executor.t.Fatal("helper cleanup has no fresh bounded context")
				}
				if executor.helperRemaining {
					_, err := io.WriteString(stdout, volumeTestHelperID)
					return err
				}
				return nil
			}
			executor.consumerChecks++
			for _, consumer := range executor.consumers {
				if _, err := io.WriteString(stdout, consumer.ID+"\n"); err != nil {
					return err
				}
			}
			return nil
		case "inspect":
			for _, consumer := range executor.consumers {
				if err := json.NewEncoder(stdout).Encode(consumer); err != nil {
					return err
				}
			}
			return nil
		case "rm":
			if !reflect.DeepEqual(args, []string{"container", "rm", "--force", "--", volumeTestHelperID}) {
				executor.t.Fatalf("cleanup did not use exact owned ID: %#v", args)
			}
			if executor.cleanupFailure {
				return errors.New("private cleanup failure")
			}
			executor.helperRemaining = false
			executor.removedHelpers++
			return nil
		}
	}
	if args[0] == "run" {
		executor.transferred = true
		executor.helperRemaining = true
		if stdin == nil {
			file, ok := stdout.(*os.File)
			if !ok {
				executor.t.Fatal("binary export payload was buffered")
			}
			info, err := file.Stat()
			if err != nil || info.Mode().Perm() != 0o600 {
				executor.t.Fatal("raw export stage is not private")
			}
			if _, err := stdout.Write(executor.nativeTar); err != nil {
				return err
			}
		} else {
			if _, ok := stdin.(*os.File); !ok {
				executor.t.Fatal("restore did not stream its private validated stage")
			}
			var err error
			executor.restoredTar, err = io.ReadAll(stdin)
			if err != nil {
				return err
			}
		}
		if executor.afterHelper != nil {
			executor.afterHelper()
		}
		if executor.helperFailure != nil {
			return executor.helperFailure()
		}
		_, _ = io.WriteString(stderr, executor.helperDiagnostics)
		executor.helperRemaining = false
		return nil
	}
	executor.t.Fatalf("unexpected Docker command: %#v", args)
	return nil
}

func (executor *volumeTestExecutor) helperCommand(t *testing.T) compose.Command {
	t.Helper()
	for _, command := range executor.commands {
		if command.Args[0] == "run" {
			return command
		}
	}
	t.Fatal("no transfer helper command")
	return compose.Command{}
}
func containsVolumeArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
func volumeArgAfter(args []string, want string) string {
	for i, arg := range args {
		if arg == want && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
func assertVolumeTemporaryEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("private temporary stages leaked: %v, %v", entries, err)
	}
}
func volumeOrchestrationArchive(t *testing.T, native []byte) string {
	t.Helper()
	directory := t.TempDir()
	raw := filepath.Join(directory, "native.tar")
	if err := os.WriteFile(raw, native, 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(directory, "archive.tar.gz")
	if _, err := packVolumeArchive(context.Background(), raw, archive, VolumeManifest{FormatVersion: VolumeFormatVersion, CreatedAt: time.Now().UTC(), SourceVolume: "source_data", Driver: "local"}); err != nil {
		t.Fatal(err)
	}
	return archive
}

package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sshdock/sshdock/internal/compose"
)

const volumeHelperImage = "debian:trixie-20261020-slim@sha256:62bd423aeee912639ca8ee681c2b762cb3e25137e0a92b20be455898a9f034ad"
const volumeOperationLabel = "sshdock.backup.operation"
const volumeMetadataLimit = 1024 * 1024
const volumeCleanupTimeout = 10 * time.Second

var volumeNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
var volumeContainerIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// VolumeCommandExecutor keeps archive bytes separate from command diagnostics.
type VolumeCommandExecutor interface {
	RunAttached(context.Context, compose.Command, io.Reader, io.Writer, io.Writer) error
}

type VolumeExportRequest struct {
	VolumeName  string
	ArchivePath string
	Quiesced    bool
	Executor    VolumeCommandExecutor
}

type VolumeRestoreRequest struct {
	VolumeName  string
	ArchivePath string
	Executor    VolumeCommandExecutor
}

func ExportVolume(ctx context.Context, request VolumeExportRequest) (inspection VolumeInspection, err error) {
	if err := validateVolumeRequest(request.VolumeName, request.ArchivePath); err != nil {
		return inspection, err
	}
	if !request.Quiesced {
		return inspection, fmt.Errorf("volume export requires --quiesced: stop all writers and prevent deployments or external writes for the entire copy")
	}
	executor := volumeExecutor(request.Executor)
	if _, err := inspectOrdinaryVolume(ctx, executor, request.VolumeName); err != nil {
		return inspection, err
	}
	if err := rejectVolumeWriters(ctx, executor, request.VolumeName); err != nil {
		return inspection, err
	}
	if _, err := volumeMetadata(ctx, executor, "pull backup helper image", "pull", volumeHelperImage); err != nil {
		return inspection, err
	}
	// Pulling may take time; observe consumers again immediately before copying.
	if err := rejectVolumeWriters(ctx, executor, request.VolumeName); err != nil {
		return inspection, err
	}
	raw, err := os.CreateTemp("", ".sshdock-volume-export-*.tar")
	if err != nil {
		return inspection, fmt.Errorf("create private volume export stage: %w", err)
	}
	defer func() {
		_ = raw.Close()
		if removeErr := os.Remove(raw.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove private volume export stage: %w", removeErr))
		}
	}()
	operation, err := newVolumeOperation(executor)
	if err != nil {
		return inspection, err
	}
	if err := operation.runHelper(ctx, request.VolumeName, false, nil, raw); err != nil {
		return inspection, err
	}
	if err := raw.Close(); err != nil {
		return inspection, fmt.Errorf("close volume export stage: %w", err)
	}
	if _, err := inspectOrdinaryVolume(ctx, executor, request.VolumeName); err != nil {
		return inspection, err
	}
	if err := rejectVolumeWriters(ctx, executor, request.VolumeName); err != nil {
		return inspection, err
	}
	if err := os.MkdirAll(filepath.Dir(request.ArchivePath), 0o700); err != nil {
		return inspection, fmt.Errorf("create volume archive directory: %w", err)
	}
	return packVolumeArchive(ctx, raw.Name(), request.ArchivePath, VolumeManifest{
		FormatVersion: VolumeFormatVersion,
		CreatedAt:     time.Now().UTC(),
		SourceVolume:  request.VolumeName,
		Driver:        "local",
	})
}

func RestoreVolume(ctx context.Context, request VolumeRestoreRequest) (inspection VolumeInspection, err error) {
	if err := validateVolumeRequest(request.VolumeName, request.ArchivePath); err != nil {
		return inspection, err
	}
	// Validate and normalize every archive byte before any Docker mutation.
	staged, err := stageVolumeArchive(ctx, request.ArchivePath)
	if err != nil {
		return inspection, err
	}
	defer func() { err = errors.Join(err, staged.Close()) }()
	executor := volumeExecutor(request.Executor)
	if exists, err := volumeExists(ctx, executor, request.VolumeName); err != nil {
		return inspection, err
	} else if exists {
		return inspection, fmt.Errorf("Docker volume %q already exists; restore requires a new volume name and never overwrites an existing volume", request.VolumeName)
	}
	if _, err := volumeMetadata(ctx, executor, "pull backup helper image", "pull", volumeHelperImage); err != nil {
		return inspection, err
	}
	operation, err := newVolumeOperation(executor)
	if err != nil {
		return inspection, err
	}
	keepVolume := false
	defer func() {
		if !keepVolume {
			err = errors.Join(err, operation.cleanupVolume(request.VolumeName))
		}
	}()
	if _, err := volumeMetadata(ctx, executor, "create fresh restore volume", "volume", "create", "--driver", "local", "--label", volumeOperationLabel+"="+operation.token, "--", request.VolumeName); err != nil {
		return inspection, err
	}
	volume, err := inspectOrdinaryVolume(ctx, executor, request.VolumeName)
	if err != nil {
		return inspection, err
	}
	if volume.Operation != operation.token {
		return inspection, fmt.Errorf("Docker volume %q was created by another operation; no data was restored or removed", request.VolumeName)
	}
	if err := rejectVolumeWriters(ctx, executor, request.VolumeName); err != nil {
		return inspection, err
	}
	raw, err := os.Open(staged.TarPath)
	if err != nil {
		return inspection, fmt.Errorf("open validated volume restore stage: %w", err)
	}
	defer raw.Close()
	if err := operation.runHelper(ctx, request.VolumeName, true, raw, io.Discard); err != nil {
		return inspection, err
	}
	if err := rejectVolumeWriters(ctx, executor, request.VolumeName); err != nil {
		return inspection, err
	}
	keepVolume = true
	return staged.Inspection, nil
}

func validateVolumeRequest(name, archivePath string) error {
	if len(name) > 255 || !volumeNamePattern.MatchString(name) {
		return fmt.Errorf("an exact Docker named-volume name of at most 255 bytes is required (start with a letter or digit; use only letters, digits, '.', '_' and '-')")
	}
	if strings.TrimSpace(archivePath) == "" {
		return fmt.Errorf("volume archive path is required")
	}
	return nil
}

func volumeExecutor(executor VolumeCommandExecutor) VolumeCommandExecutor {
	if executor == nil {
		return compose.LocalCommandExecutor{}
	}
	return executor
}

type dockerVolumeMetadata struct {
	Name      string            `json:"name"`
	Driver    string            `json:"driver"`
	Scope     string            `json:"scope"`
	Options   map[string]string `json:"options"`
	Operation string            `json:"operation"`
}

const volumeInspectTemplate = `{"name":{{json .Name}},"driver":{{json .Driver}},"scope":{{json .Scope}},"options":{{json .Options}},"operation":{{json (index .Labels "sshdock.backup.operation")}}}`

func inspectOrdinaryVolume(ctx context.Context, executor VolumeCommandExecutor, name string) (dockerVolumeMetadata, error) {
	data, err := volumeMetadata(ctx, executor, "inspect Docker volume", "volume", "inspect", "--format", volumeInspectTemplate, "--", name)
	if err != nil {
		return dockerVolumeMetadata{}, err
	}
	var volume dockerVolumeMetadata
	if err := json.Unmarshal(data, &volume); err != nil || volume.Name != name {
		return volume, fmt.Errorf("Docker returned invalid metadata for volume %q", name)
	}
	if volume.Driver != "local" || volume.Scope != "local" || len(volume.Options) != 0 {
		return volume, fmt.Errorf("Docker volume %q is not an ordinary local volume; custom drivers, remote scopes and driver options (including bind/NFS mounts) are unsupported", name)
	}
	return volume, nil
}

func volumeExists(ctx context.Context, executor VolumeCommandExecutor, name string) (bool, error) {
	data, err := volumeMetadata(ctx, executor, "list Docker volume names", "volume", "ls", "--format", "{{.Name}}")
	if err != nil {
		return false, err
	}
	for _, candidate := range strings.Fields(string(data)) {
		if !volumeNamePattern.MatchString(candidate) {
			return false, fmt.Errorf("Docker returned an invalid volume name")
		}
		if candidate == name {
			return true, nil
		}
	}
	return false, nil
}

const volumeConsumerTemplate = `{"id":{{json .Id}},"running":{{json .State.Running}},"mounts":{{json .Mounts}}}`

func rejectVolumeWriters(ctx context.Context, executor VolumeCommandExecutor, name string) error {
	data, err := volumeMetadata(ctx, executor, "list Docker volume consumers", "container", "ls", "--all", "--no-trunc", "--filter", "volume="+name, "--format", "{{.ID}}")
	if err != nil {
		return err
	}
	ids, err := volumeContainerIDs(data)
	if err != nil || len(ids) == 0 {
		return err
	}
	args := append([]string{"container", "inspect", "--format", volumeConsumerTemplate, "--"}, ids...)
	data, err = volumeMetadata(ctx, executor, "inspect Docker volume consumers", args...)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	seen := 0
	for {
		var container struct {
			ID      string `json:"id"`
			Running bool   `json:"running"`
			Mounts  []struct {
				Type string `json:"Type"`
				Name string `json:"Name"`
				RW   bool   `json:"RW"`
			} `json:"mounts"`
		}
		if err := decoder.Decode(&container); errors.Is(err, io.EOF) {
			break
		} else if err != nil || !volumeContainerIDPattern.MatchString(container.ID) {
			return fmt.Errorf("Docker returned invalid volume consumer metadata")
		}
		seen++
		for _, mount := range container.Mounts {
			if container.Running && mount.Type == "volume" && mount.Name == name && mount.RW {
				return fmt.Errorf("Docker volume %q has running read/write container %s; stop all writers and prevent restarts or deployments before retrying", name, container.ID)
			}
		}
	}
	if seen != len(ids) {
		return fmt.Errorf("Docker volume consumer inspection was incomplete; retry after stopping all writers")
	}
	return nil
}

type volumeOperation struct {
	executor VolumeCommandExecutor
	token    string
	helper   string
}

func newVolumeOperation(executor VolumeCommandExecutor) (*volumeOperation, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, fmt.Errorf("create volume operation identity: %w", err)
	}
	token := hex.EncodeToString(random[:])
	return &volumeOperation{executor: executor, token: token, helper: "sshdock-volume-" + token}, nil
}

func (operation *volumeOperation) runHelper(ctx context.Context, name string, restore bool, stdin io.Reader, stdout io.Writer) (err error) {
	defer func() { err = errors.Join(err, operation.cleanupHelper()) }()
	mount := "type=volume,src=" + name + ",dst=/data,volume-nocopy"
	args := []string{"run", "--rm", "--name", operation.helper, "--label", volumeOperationLabel + "=" + operation.token,
		"--pull=never", "--network", "none", "--read-only", "--user", "0:0", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true"}
	if restore {
		args = append(args, "--interactive", "--cap-add", "CHOWN", "--cap-add", "DAC_OVERRIDE", "--cap-add", "FOWNER")
	} else {
		mount += ",readonly"
		args = append(args, "--cap-add", "DAC_READ_SEARCH")
	}
	args = append(args, "--mount", mount, volumeHelperImage, "tar")
	if restore {
		args = append(args, "--numeric-owner", "--same-owner", "--same-permissions", "--delay-directory-restore", "--no-acls", "--no-xattrs", "--no-selinux", "-C", "/data", "-xf", "-")
	} else {
		args = append(args, "--format=pax", "--numeric-owner", "--acls", "--xattrs", "--xattrs-include=*", "--selinux", "--sparse", "-C", "/data", "-cf", "-", ".")
	}
	diagnostics := volumeBoundedBuffer{limit: 4096}
	if err := operation.executor.RunAttached(ctx, compose.Command{Name: "docker", Args: args}, stdin, stdout, &diagnostics); err != nil {
		return &volumeCommandError{action: "volume transfer helper", cause: err}
	}
	if diagnostics.total != 0 {
		return fmt.Errorf("volume transfer helper emitted diagnostics; no completed backup or restore is accepted: check file readability and unsupported filesystem metadata")
	}
	return nil
}

func (operation *volumeOperation) cleanupHelper() error {
	ctx, cancel := context.WithTimeout(context.Background(), volumeCleanupTimeout)
	defer cancel()
	data, err := volumeMetadata(ctx, operation.executor, "find owned volume helper for cleanup", "container", "ls", "--all", "--no-trunc", "--filter", "name=^/"+operation.helper+"$", "--filter", "label="+volumeOperationLabel+"="+operation.token, "--format", "{{.ID}}")
	if err == nil {
		var ids []string
		ids, err = volumeContainerIDs(data)
		if err == nil && len(ids) != 0 {
			_, err = volumeMetadata(ctx, operation.executor, "remove owned volume helper", append([]string{"container", "rm", "--force", "--"}, ids...)...)
		}
	}
	if err != nil {
		return fmt.Errorf("volume helper cleanup incomplete for %q; inspect containers with label %s=%s before removing only this operation's helper: %w", operation.helper, volumeOperationLabel, operation.token, err)
	}
	return nil
}

func (operation *volumeOperation) cleanupVolume(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), volumeCleanupTimeout)
	defer cancel()
	exists, err := volumeExists(ctx, operation.executor, name)
	if err == nil && !exists {
		return nil
	}
	if err == nil {
		var volume dockerVolumeMetadata
		volume, err = inspectOrdinaryVolume(ctx, operation.executor, name)
		if err == nil && volume.Operation != operation.token {
			return nil
		}
		if err == nil {
			_, err = volumeMetadata(ctx, operation.executor, "remove incomplete owned restore volume", "volume", "rm", "--", name)
		}
	}
	if err != nil {
		return fmt.Errorf("restore cleanup incomplete for volume %q; inspect label %s=%s and remove this operation's incomplete volume only after detaching its consumers: %w", name, volumeOperationLabel, operation.token, err)
	}
	return nil
}

func volumeContainerIDs(data []byte) ([]string, error) {
	ids := strings.Fields(string(data))
	for _, id := range ids {
		if !volumeContainerIDPattern.MatchString(id) {
			return nil, fmt.Errorf("Docker returned an invalid container identity")
		}
	}
	return ids, nil
}

// A bounded capture never includes native output in returned errors. Metadata
// can contain private mount options; archive bytes must never enter this path.
func volumeMetadata(ctx context.Context, executor VolumeCommandExecutor, action string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, &volumeCommandError{action: action, cause: err}
	}
	stdout := volumeBoundedBuffer{limit: volumeMetadataLimit}
	stderr := volumeBoundedBuffer{limit: 4096}
	if err := executor.RunAttached(ctx, compose.Command{Name: "docker", Args: args}, nil, &stdout, &stderr); err != nil {
		return nil, &volumeCommandError{action: action, cause: err}
	}
	if stdout.total > stdout.limit {
		return nil, fmt.Errorf("%s returned more than %d bytes of Docker metadata", action, stdout.limit)
	}
	return stdout.buffer.Bytes(), nil
}

type volumeBoundedBuffer struct {
	buffer bytes.Buffer
	limit  int
	total  int
}

func (buffer *volumeBoundedBuffer) Write(data []byte) (int, error) {
	n := len(data)
	buffer.total += n
	if remaining := buffer.limit - buffer.buffer.Len(); remaining > 0 {
		_, _ = buffer.buffer.Write(data[:min(n, remaining)])
	}
	return n, nil
}

type volumeCommandError struct {
	action string
	cause  error
}

func (err *volumeCommandError) Error() string {
	if errors.Is(err.cause, context.Canceled) {
		return err.action + " cancelled"
	}
	if errors.Is(err.cause, context.DeadlineExceeded) {
		return err.action + " timed out; inspect Docker before retrying"
	}
	return err.action + " failed; verify Docker access, the pinned helper image and available disk space"
}

func (err *volumeCommandError) Unwrap() error { return err.cause }

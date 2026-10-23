package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const VolumeFormatVersion = "sshdock-volume/v1"

const (
	maxVolumeManifestBytes = 64 << 10
	maxVolumeEntries       = 100_000
	maxVolumePathBytes     = 4096
	maxVolumeNamesBytes    = 16 << 20
	maxVolumeMetadataBytes = 64 << 20
)

type VolumeManifest struct {
	FormatVersion string    `json:"format_version"`
	CreatedAt     time.Time `json:"created_at"`
	SourceVolume  string    `json:"source_volume"`
	Driver        string    `json:"driver"`
}

type VolumeInspection struct {
	Path           string         `json:"path"`
	Manifest       VolumeManifest `json:"manifest"`
	FileCount      int            `json:"file_count"`
	DirectoryCount int            `json:"directory_count"`
	TotalBytes     int64          `json:"total_bytes"`
}

type stagedVolumeArchive struct {
	Inspection VolumeInspection
	TarPath    string
}

func (s *stagedVolumeArchive) Close() error {
	if s.TarPath == "" {
		return nil
	}
	if err := os.Remove(s.TarPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove staged volume data: %w", err)
	}
	s.TarPath = ""
	return nil
}

// InspectVolume validates the complete archive, including its gzip checksum,
// without extracting data or creating a payload-sized temporary file.
func InspectVolume(ctx context.Context, archivePath string) (VolumeInspection, error) {
	return readVolumeArchive(ctx, archivePath, nil)
}

func packVolumeArchive(ctx context.Context, nativeTarPath, destination string, manifest VolumeManifest) (VolumeInspection, error) {
	if err := ctx.Err(); err != nil {
		return VolumeInspection{}, err
	}
	if err := validateVolumeManifest(manifest); err != nil {
		return VolumeInspection{}, err
	}
	if strings.TrimSpace(destination) == "" {
		return VolumeInspection{}, fmt.Errorf("volume archive destination is required")
	}
	if err := requireRegularFile(nativeTarPath, "native volume tar"); err != nil {
		return VolumeInspection{}, err
	}
	source, err := os.Open(nativeTarPath)
	if err != nil {
		return VolumeInspection{}, fmt.Errorf("open native volume tar: %w", err)
	}
	defer source.Close()
	file, err := os.CreateTemp(filepath.Dir(destination), ".sshdock-volume-*.tar.gz")
	if err != nil {
		return VolumeInspection{}, fmt.Errorf("create private volume archive in destination directory: %w", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	gz := gzip.NewWriter(file)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	metadata, err := json.Marshal(manifest)
	if err != nil {
		return VolumeInspection{}, fmt.Errorf("encode volume manifest: %w", err)
	}
	if len(metadata) > maxVolumeManifestBytes {
		return VolumeInspection{}, fmt.Errorf("volume manifest exceeds %d bytes", maxVolumeManifestBytes)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(metadata)), ModTime: manifest.CreatedAt}); err != nil {
		return VolumeInspection{}, fmt.Errorf("write volume manifest header: %w", err)
	}
	if _, err := tw.Write(metadata); err != nil {
		return VolumeInspection{}, fmt.Errorf("write volume manifest: %w", err)
	}
	input := &volumeTarInput{ctx: ctx, reader: source}
	inspection, err := copyVolumeEntries(input, tar.NewReader(input), tw, ".", "data", 0)
	if err != nil {
		return VolumeInspection{}, err
	}
	if err := tw.Close(); err != nil {
		return VolumeInspection{}, fmt.Errorf("finish volume tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return VolumeInspection{}, fmt.Errorf("finish volume gzip: %w", err)
	}
	if err := file.Sync(); err != nil {
		return VolumeInspection{}, fmt.Errorf("sync volume archive: %w", err)
	}
	if err := file.Close(); err != nil {
		return VolumeInspection{}, fmt.Errorf("close volume archive: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return VolumeInspection{}, err
	}
	// A hard link publishes the completed private file atomically and refuses
	// every existing destination, including a symlink. Rename would overwrite it.
	if err := os.Link(file.Name(), destination); err != nil {
		return VolumeInspection{}, fmt.Errorf("publish volume archive without replacing destination: %w", err)
	}
	if err := os.Remove(file.Name()); err != nil {
		return VolumeInspection{}, fmt.Errorf("archive published but remove private temporary link: %w", err)
	}
	inspection.Path = destination
	inspection.Manifest = manifest
	return inspection, nil
}

func stageVolumeArchive(ctx context.Context, archivePath string) (*stagedVolumeArchive, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", ".sshdock-volume-restore-*.tar")
	if err != nil {
		return nil, fmt.Errorf("create private staged volume tar: %w", err)
	}
	stage := &stagedVolumeArchive{TarPath: file.Name()}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = stage.Close()
		}
	}()
	tw := tar.NewWriter(file)
	defer tw.Close()
	inspection, err := readVolumeArchive(ctx, archivePath, tw)
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("finish staged volume tar: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close staged volume tar: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stage.Inspection = inspection
	keep = true
	return stage, nil
}

func readVolumeArchive(ctx context.Context, archivePath string, output *tar.Writer) (VolumeInspection, error) {
	if err := ctx.Err(); err != nil {
		return VolumeInspection{}, err
	}
	if err := requireRegularFile(archivePath, "volume archive"); err != nil {
		return VolumeInspection{}, err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return VolumeInspection{}, fmt.Errorf("open volume archive: %w", err)
	}
	defer file.Close()
	buffer := bufio.NewReader(&volumeTarInput{ctx: ctx, reader: file})
	gz, err := gzip.NewReader(buffer)
	if err != nil {
		return VolumeInspection{}, fmt.Errorf("read volume gzip: %w", err)
	}
	defer gz.Close()
	gz.Multistream(false)
	input := &volumeTarInput{ctx: ctx, reader: gz, metadata: true}
	tr := tar.NewReader(input)
	header, err := tr.Next()
	if err != nil {
		return VolumeInspection{}, fmt.Errorf("read volume manifest header: %w", err)
	}
	if header.Name != "manifest.json" || header.Typeflag != tar.TypeReg {
		return VolumeInspection{}, fmt.Errorf("volume archive must begin with regular manifest.json")
	}
	if err := validateVolumeHeader(header); err != nil {
		return VolumeInspection{}, err
	}
	if header.Size <= 0 || header.Size > maxVolumeManifestBytes {
		return VolumeInspection{}, fmt.Errorf("volume manifest must contain 1..%d bytes", maxVolumeManifestBytes)
	}
	metadata, err := io.ReadAll(tr)
	if err != nil {
		return VolumeInspection{}, fmt.Errorf("read volume manifest: %w", err)
	}
	var manifest VolumeManifest
	decoder := json.NewDecoder(bytes.NewReader(metadata))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return VolumeInspection{}, fmt.Errorf("decode volume manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return VolumeInspection{}, fmt.Errorf("volume manifest must contain one JSON object")
	}
	if err := validateVolumeManifest(manifest); err != nil {
		return VolumeInspection{}, err
	}
	inspection, err := copyVolumeEntries(input, tr, output, "data", ".", header.Size)
	if err != nil {
		return VolumeInspection{}, err
	}
	// copyVolumeEntries consumes all remaining decompressed padding, forcing
	// gzip to verify its footer. Close alone never checks the gzip checksum.
	if _, err := buffer.ReadByte(); err != io.EOF {
		if err != nil {
			return VolumeInspection{}, fmt.Errorf("read after volume gzip: %w", err)
		}
		return VolumeInspection{}, fmt.Errorf("volume archive contains trailing data or concatenated gzip members")
	}
	inspection.Path = archivePath
	inspection.Manifest = manifest
	if err := ctx.Err(); err != nil {
		return VolumeInspection{}, err
	}
	return inspection, nil
}

// Metadata and payload share a reader, but only header/padding reads consume
// the metadata budget. This also bounds hidden PAX/GNU extension headers.
type volumeTarInput struct {
	ctx           context.Context
	reader        io.Reader
	read          int64
	metadata      bool
	metadataBytes int64
}

func (r *volumeTarInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.metadata {
		remaining := int64(maxVolumeMetadataBytes) - r.metadataBytes
		if remaining <= 0 {
			return 0, fmt.Errorf("volume tar metadata exceeds %d bytes", maxVolumeMetadataBytes)
		}
		if int64(len(p)) > remaining {
			p = p[:remaining]
		}
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	if r.metadata {
		r.metadataBytes += int64(n)
	}
	return n, err
}

func copyVolumeEntries(input *volumeTarInput, tr *tar.Reader, output *tar.Writer, sourceRoot, targetRoot string, previousSize int64) (VolumeInspection, error) {
	inspection := VolumeInspection{}
	seen := make(map[string]bool) // true denotes a directory
	ancestors := make(map[string]bool)
	namesBytes := 0
	for {
		input.metadata = true
		before := input.read
		header, err := tr.Next()
		if err == io.EOF {
			padding := (512 - previousSize%512) % 512
			if input.read-before != padding+1024 {
				return VolumeInspection{}, fmt.Errorf("volume tar is missing its complete end marker")
			}
			if err := consumeVolumePadding(input); err != nil {
				return VolumeInspection{}, err
			}
			if !seen["."] {
				return VolumeInspection{}, fmt.Errorf("volume tar is missing its data root directory")
			}
			return inspection, nil
		}
		if err != nil {
			return VolumeInspection{}, fmt.Errorf("read volume tar: %w", err)
		}
		if err := validateVolumeHeader(header); err != nil {
			return VolumeInspection{}, err
		}
		name, err := volumeEntryName(header.Name, sourceRoot, header.Typeflag == tar.TypeDir)
		if err != nil {
			return VolumeInspection{}, err
		}
		if _, exists := seen[name]; exists {
			return VolumeInspection{}, fmt.Errorf("duplicate volume entry %q", name)
		}
		isDir := header.Typeflag == tar.TypeDir
		if name == "." && !isDir {
			return VolumeInspection{}, fmt.Errorf("volume data root must be a directory")
		}
		if !isDir && ancestors[name] {
			return VolumeInspection{}, fmt.Errorf("volume file %q conflicts with an existing descendant", name)
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if dir, exists := seen[parent]; exists {
				if !dir {
					return VolumeInspection{}, fmt.Errorf("volume entry %q has file ancestor %q", name, parent)
				}
				continue
			}
			if !ancestors[parent] {
				namesBytes += len(parent)
				if len(seen)+len(ancestors) >= maxVolumeEntries || namesBytes > maxVolumeNamesBytes {
					return VolumeInspection{}, fmt.Errorf("volume entry count or combined path metadata exceeds supported limit")
				}
				ancestors[parent] = true
			}
		}
		if ancestors[name] {
			delete(ancestors, name)
			namesBytes -= len(name)
		}
		namesBytes += len(name)
		if len(seen)+len(ancestors) >= maxVolumeEntries || namesBytes > maxVolumeNamesBytes {
			return VolumeInspection{}, fmt.Errorf("volume entry count or combined path metadata exceeds supported limit")
		}
		seen[name] = isDir
		if isDir {
			inspection.DirectoryCount++
		} else {
			inspection.FileCount++
			if header.Size > math.MaxInt64-inspection.TotalBytes {
				return VolumeInspection{}, fmt.Errorf("volume content size overflows supported limit")
			}
			inspection.TotalBytes += header.Size
		}
		var destination io.Writer = io.Discard
		if output != nil {
			outputName := targetRoot
			if name != "." {
				outputName = path.Join(targetRoot, name)
			}
			if isDir {
				outputName += "/"
			}
			normalized := &tar.Header{Name: outputName, Typeflag: header.Typeflag, Mode: header.Mode, Uid: header.Uid, Gid: header.Gid, Size: header.Size, ModTime: header.ModTime, Format: tar.FormatPAX}
			if err := output.WriteHeader(normalized); err != nil {
				return VolumeInspection{}, fmt.Errorf("write normalized volume header: %w", err)
			}
			destination = output
		}
		input.metadata = false
		if _, err := io.CopyN(destination, tr, header.Size); err != nil {
			return VolumeInspection{}, fmt.Errorf("copy volume entry %q: %w", name, err)
		}
		previousSize = header.Size
	}
}

func consumeVolumePadding(input *volumeTarInput) error {
	var buffer [32 << 10]byte
	for {
		n, err := input.Read(buffer[:])
		for _, value := range buffer[:n] {
			if value != 0 {
				return fmt.Errorf("volume tar contains nonzero trailing data")
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("finish volume tar and checksum: %w", err)
		}
	}
}

func volumeEntryName(raw, root string, directory bool) (string, error) {
	if len(raw) > maxVolumePathBytes+8 || !utf8.ValidString(raw) || strings.Contains(raw, "\\") || strings.HasPrefix(raw, "/") || strings.ContainsFunc(raw, func(value rune) bool { return value < 0x20 || value == 0x7f }) {
		return "", fmt.Errorf("unsafe or oversized volume path %q", raw)
	}
	name := strings.TrimPrefix(raw, "./")
	if raw == "./" && directory {
		name = "."
	}
	if directory {
		name = strings.TrimSuffix(name, "/")
	}
	if root == "data" {
		if name == "data" {
			name = "."
		} else if strings.HasPrefix(name, "data/") {
			name = strings.TrimPrefix(name, "data/")
		} else {
			return "", fmt.Errorf("volume archive entry %q is outside data/", raw)
		}
	}
	if name == "." && directory {
		return ".", nil
	}
	if len(name) == 0 || len(name) > maxVolumePathBytes {
		return "", fmt.Errorf("unsafe or oversized volume path %q", raw)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("unsafe volume path %q", raw)
		}
	}
	return name, nil
}

func validateVolumeHeader(header *tar.Header) error {
	if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
		return fmt.Errorf("unsupported volume entry type for %q; only ordinary files and directories are supported", header.Name)
	}
	if header.Size < 0 || header.Typeflag == tar.TypeDir && header.Size != 0 {
		return fmt.Errorf("invalid volume entry size for %q", header.Name)
	}
	if header.Mode < 0 || header.Mode&^int64(0o777) != 0 {
		return fmt.Errorf("unsupported special permission bits for volume entry %q", header.Name)
	}
	if header.Uid < 0 || header.Gid < 0 || int64(header.Uid) >= math.MaxUint32 || int64(header.Gid) >= math.MaxUint32 {
		return fmt.Errorf("unsupported numeric ownership for volume entry %q", header.Name)
	}
	if header.Linkname != "" || header.Devmajor != 0 || header.Devminor != 0 || len(header.Xattrs) != 0 {
		return fmt.Errorf("unsupported link, device or extended metadata for volume entry %q", header.Name)
	}
	for key := range header.PAXRecords {
		switch key {
		case "path", "size", "uid", "gid", "uname", "gname", "mtime", "atime", "ctime":
		default:
			return fmt.Errorf("unsupported volume metadata %q for %q", key, header.Name)
		}
	}
	return nil
}

func validateVolumeManifest(manifest VolumeManifest) error {
	if manifest.FormatVersion != VolumeFormatVersion {
		return fmt.Errorf("unsupported volume archive format %q", manifest.FormatVersion)
	}
	if manifest.CreatedAt.IsZero() {
		return fmt.Errorf("volume manifest created_at is required")
	}
	if manifest.Driver != "local" {
		return fmt.Errorf("unsupported volume driver %q; only local named volumes are supported", manifest.Driver)
	}
	if len(manifest.SourceVolume) == 0 || len(manifest.SourceVolume) > 255 {
		return fmt.Errorf("volume manifest source_volume is invalid")
	}
	for i, value := range []byte(manifest.SourceVolume) {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || i > 0 && (value == '_' || value == '.' || value == '-') {
			continue
		}
		return fmt.Errorf("volume manifest source_volume is invalid")
	}
	return nil
}

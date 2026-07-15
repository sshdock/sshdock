package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sshdock/sshdock/internal/config"
)

const FormatVersion = "sshdock-backup/v1"

type CreateRequest struct {
	Config         config.Config
	Destination    string
	IncludeVolumes bool
	VolumeLister   VolumeLister
	Now            func() time.Time
}

type RestoreRequest struct {
	Config      config.Config
	ArchivePath string
}

type CreateResult struct {
	Path        string
	FileCount   int
	VolumeCount int
}

type RestoreResult struct {
	DataDir string
}

type Inspection struct {
	Path        string
	Manifest    Manifest
	FileCount   int
	VolumeCount int
	Volumes     []Volume
}

type Manifest struct {
	FormatVersion     string      `json:"format_version"`
	CreatedAt         time.Time   `json:"created_at"`
	Source            SourcePaths `json:"source"`
	Entries           []Entry     `json:"entries"`
	RestoreGuardrails []string    `json:"restore_guardrails"`
}

type SourcePaths struct {
	DataDir             string `json:"data_dir"`
	SQLiteDBPath        string `json:"sqlite_db_path"`
	ConfigKeyPath       string `json:"config_key_path"`
	GitAuthorizedKeys   string `json:"git_authorized_keys_path"`
	OperatorHostKey     string `json:"dashboard_host_key_path"`
	OperatorKeys        string `json:"dashboard_authorized_keys_path"`
	CaddyConfigPath     string `json:"caddy_config_path"`
	CaddyMainConfigPath string `json:"caddy_main_config_path"`
}

type Entry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Mode string `json:"mode"`
	Size int64  `json:"size"`
}

type Volume struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver,omitempty"`
	Mountpoint string            `json:"Mountpoint,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
}

type VolumeLister interface {
	ListVolumes(ctx context.Context) ([]Volume, error)
}

type LocalDockerVolumeLister struct{}

func Create(ctx context.Context, request CreateRequest) (CreateResult, error) {
	if request.IncludeVolumes {
		return CreateResult{}, fmt.Errorf("Docker volume content backup is not implemented; SSHDock backup archives include Docker volume inventory only")
	}
	if err := request.Config.Validate(); err != nil {
		return CreateResult{}, err
	}
	if request.Now == nil {
		request.Now = func() time.Time { return time.Now().UTC() }
	}
	destination := request.Destination
	if destination == "" {
		destination = DefaultArchivePath(request.Config, request.Now())
	}
	if err := requireRegularFile(request.Config.SQLiteDBPath, "SQLite database"); err != nil {
		return CreateResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return CreateResult{}, fmt.Errorf("create backup destination directory: %w", err)
	}

	lister := request.VolumeLister
	if lister == nil {
		lister = LocalDockerVolumeLister{}
	}
	volumes, err := lister.ListVolumes(ctx)
	if err != nil {
		return CreateResult{}, fmt.Errorf("list Docker volumes for backup inventory: %w", err)
	}

	tempPath := destination + ".tmp"
	_ = os.Remove(tempPath)
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return CreateResult{}, fmt.Errorf("create backup archive: %w", err)
	}
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	writer := archiveWriter{tar: tarWriter}

	if err := addTree(&writer, request.Config.DataDir, "data", createExcludes(request.Config, destination)); err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		_ = file.Close()
		return CreateResult{}, err
	}
	if err := addOptionalFile(&writer, request.Config.CaddyConfigPath, "caddy/generated.caddyfile"); err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		_ = file.Close()
		return CreateResult{}, err
	}
	if err := addOptionalFile(&writer, request.Config.CaddyMainConfigPath, "caddy/main.Caddyfile"); err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		_ = file.Close()
		return CreateResult{}, err
	}
	volumeData, err := json.MarshalIndent(volumes, "", "  ")
	if err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		_ = file.Close()
		return CreateResult{}, fmt.Errorf("encode Docker volume inventory: %w", err)
	}
	if err := writer.addBytes("docker/volumes.json", volumeData, 0o644, request.Now()); err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		_ = file.Close()
		return CreateResult{}, err
	}

	manifest := Manifest{
		FormatVersion:     FormatVersion,
		CreatedAt:         request.Now(),
		Source:            sourcePaths(request.Config),
		Entries:           append([]Entry(nil), writer.entries...),
		RestoreGuardrails: defaultRestoreGuardrails(),
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		_ = file.Close()
		return CreateResult{}, fmt.Errorf("encode backup manifest: %w", err)
	}
	if err := writer.addBytes("manifest.json", manifestData, 0o644, request.Now()); err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		_ = file.Close()
		return CreateResult{}, err
	}

	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		_ = file.Close()
		return CreateResult{}, fmt.Errorf("finish backup tar: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		_ = file.Close()
		return CreateResult{}, fmt.Errorf("finish backup gzip: %w", err)
	}
	if err := file.Close(); err != nil {
		return CreateResult{}, fmt.Errorf("close backup archive: %w", err)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return CreateResult{}, fmt.Errorf("move backup archive into place: %w", err)
	}
	removeTemp = false

	return CreateResult{Path: destination, FileCount: writer.fileCount, VolumeCount: len(volumes)}, nil
}

func Inspect(_ context.Context, archivePath string) (Inspection, error) {
	manifest, volumes, fileCount, err := readArchiveMetadata(archivePath)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{
		Path:        archivePath,
		Manifest:    manifest,
		FileCount:   fileCount,
		VolumeCount: len(volumes),
		Volumes:     volumes,
	}, nil
}

func Restore(_ context.Context, request RestoreRequest) error {
	result, err := restore(request)
	if err != nil {
		return err
	}
	_ = result
	return nil
}

func restore(request RestoreRequest) (RestoreResult, error) {
	if err := request.Config.Validate(); err != nil {
		return RestoreResult{}, err
	}
	if strings.TrimSpace(request.ArchivePath) == "" {
		return RestoreResult{}, fmt.Errorf("backup archive path is required")
	}
	parent := filepath.Dir(request.Config.DataDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return RestoreResult{}, fmt.Errorf("create data dir parent: %w", err)
	}
	tempDir, err := os.MkdirTemp(parent, ".sshdock-restore-*")
	if err != nil {
		return RestoreResult{}, fmt.Errorf("create restore temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if err := extractArchive(request.ArchivePath, tempDir); err != nil {
		return RestoreResult{}, err
	}
	manifest, err := readManifestFile(filepath.Join(tempDir, "manifest.json"))
	if err != nil {
		return RestoreResult{}, err
	}
	if manifest.FormatVersion != FormatVersion {
		return RestoreResult{}, fmt.Errorf("unsupported backup format %q", manifest.FormatVersion)
	}
	dataRoot := filepath.Join(tempDir, "data")
	if err := requireRegularFile(filepath.Join(dataRoot, "sshdock.db"), "missing required archive entry data/sshdock.db"); err != nil {
		return RestoreResult{}, err
	}
	if err := validateConfigKey(filepath.Join(dataRoot, "config.key")); err != nil {
		return RestoreResult{}, err
	}
	if err := validateRestoreTargetPaths(request.Config); err != nil {
		return RestoreResult{}, err
	}

	if err := os.RemoveAll(request.Config.DataDir); err != nil {
		return RestoreResult{}, fmt.Errorf("replace data dir: %w", err)
	}
	if err := os.Rename(dataRoot, request.Config.DataDir); err != nil {
		return RestoreResult{}, fmt.Errorf("restore data dir: %w", err)
	}
	if err := restoreOptionalFile(filepath.Join(tempDir, "caddy", "generated.caddyfile"), request.Config.CaddyConfigPath); err != nil {
		return RestoreResult{}, err
	}
	if err := restoreOptionalFile(filepath.Join(tempDir, "caddy", "main.Caddyfile"), request.Config.CaddyMainConfigPath); err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{DataDir: request.Config.DataDir}, nil
}

func DefaultArchivePath(cfg config.Config, now time.Time) string {
	return filepath.Join(cfg.DataDir, "backups", "sshdock-backup-"+now.UTC().Format("20060102T150405Z")+".tar.gz")
}

func (LocalDockerVolumeLister) ListVolumes(ctx context.Context) ([]Volume, error) {
	output, err := exec.CommandContext(ctx, "docker", "volume", "ls", "--format", "{{.Name}}").Output()
	if err != nil {
		return nil, err
	}
	names := strings.Fields(string(output))
	if len(names) == 0 {
		return nil, nil
	}
	args := append([]string{"volume", "inspect"}, names...)
	inspectOutput, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, err
	}
	var volumes []Volume
	if err := json.Unmarshal(inspectOutput, &volumes); err != nil {
		return nil, err
	}
	sort.Slice(volumes, func(i, j int) bool { return volumes[i].Name < volumes[j].Name })
	return volumes, nil
}

type archiveWriter struct {
	tar       *tar.Writer
	entries   []Entry
	fileCount int
}

func addTree(writer *archiveWriter, sourceRoot string, archiveRoot string, excluded func(string) bool) error {
	info, err := os.Stat(sourceRoot)
	if err != nil {
		return fmt.Errorf("read backup source %s: %w", sourceRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("backup source %s is not a directory", sourceRoot)
	}
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if excluded(path) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		name := archiveRoot
		if rel != "." {
			name = filepath.ToSlash(filepath.Join(archiveRoot, rel))
		}
		return writer.addPath(path, name, info)
	})
}

func addOptionalFile(writer *archiveWriter, sourcePath string, archivePath string) error {
	info, err := os.Stat(sourcePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read backup source %s: %w", sourcePath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("backup source %s is not a regular file", sourcePath)
	}
	return writer.addPath(sourcePath, archivePath, info)
}

func (w *archiveWriter) addPath(path string, archivePath string, info os.FileInfo) error {
	link := ""
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return fmt.Errorf("read symlink %s: %w", path, err)
		}
		link = target
	}
	header, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return fmt.Errorf("create tar header for %s: %w", path, err)
	}
	header.Name = filepath.ToSlash(archivePath)
	if err := w.tar.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header for %s: %w", archivePath, err)
	}
	if info.Mode().IsRegular() {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open backup source %s: %w", path, err)
		}
		defer file.Close()
		if _, err := io.Copy(w.tar, file); err != nil {
			return fmt.Errorf("write backup source %s: %w", path, err)
		}
		w.fileCount++
	}
	w.entries = append(w.entries, entryFromHeader(header))
	return nil
}

func (w *archiveWriter) addBytes(archivePath string, data []byte, mode os.FileMode, modTime time.Time) error {
	header := &tar.Header{
		Name:    filepath.ToSlash(archivePath),
		Mode:    int64(mode),
		Size:    int64(len(data)),
		ModTime: modTime,
	}
	if err := w.tar.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header for %s: %w", archivePath, err)
	}
	if _, err := w.tar.Write(data); err != nil {
		return fmt.Errorf("write backup entry %s: %w", archivePath, err)
	}
	w.fileCount++
	w.entries = append(w.entries, entryFromHeader(header))
	return nil
}

func entryFromHeader(header *tar.Header) Entry {
	kind := "file"
	if header.Typeflag == tar.TypeDir {
		kind = "dir"
	} else if header.Typeflag == tar.TypeSymlink {
		kind = "symlink"
	}
	return Entry{
		Path: header.Name,
		Kind: kind,
		Mode: fmt.Sprintf("%04o", header.Mode&0o7777),
		Size: header.Size,
	}
}

func createExcludes(cfg config.Config, destination string) func(string) bool {
	cleanDestination := filepath.Clean(destination)
	backupsDir := filepath.Join(cfg.DataDir, "backups")
	return func(path string) bool {
		cleanPath := filepath.Clean(path)
		return cleanPath == cleanDestination || cleanPath == backupsDir || strings.HasPrefix(cleanPath, backupsDir+string(os.PathSeparator))
	}
}

func readArchiveMetadata(archivePath string) (Manifest, []Volume, int, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return Manifest{}, nil, 0, fmt.Errorf("open backup archive: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return Manifest{}, nil, 0, fmt.Errorf("read backup gzip: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var manifest Manifest
	var volumes []Volume
	manifestFound := false
	fileCount := 0
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Manifest{}, nil, 0, fmt.Errorf("read backup tar: %w", err)
		}
		if header.FileInfo().Mode().IsRegular() {
			fileCount++
		}
		switch header.Name {
		case "manifest.json":
			data, err := io.ReadAll(tarReader)
			if err != nil {
				return Manifest{}, nil, 0, err
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return Manifest{}, nil, 0, fmt.Errorf("decode manifest.json: %w", err)
			}
			manifestFound = true
		case "docker/volumes.json":
			data, err := io.ReadAll(tarReader)
			if err != nil {
				return Manifest{}, nil, 0, err
			}
			if err := json.Unmarshal(data, &volumes); err != nil {
				return Manifest{}, nil, 0, fmt.Errorf("decode docker/volumes.json: %w", err)
			}
		}
	}
	if !manifestFound {
		return Manifest{}, nil, 0, fmt.Errorf("backup archive missing manifest.json")
	}
	if manifest.FormatVersion != FormatVersion {
		return Manifest{}, nil, 0, fmt.Errorf("unsupported backup format %q", manifest.FormatVersion)
	}
	return manifest, volumes, fileCount, nil
}

func extractArchive(archivePath string, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open backup archive: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read backup gzip: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read backup tar: %w", err)
		}
		targetPath, err := safeJoin(destination, header.Name)
		if err != nil {
			return err
		}
		mode := os.FileMode(header.Mode)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, mode.Perm()); err != nil {
				return fmt.Errorf("restore directory %s: %w", header.Name, err)
			}
		case tar.TypeSymlink:
			if err := validateSymlinkTarget(destination, targetPath, header.Linkname); err != nil {
				return fmt.Errorf("restore symlink %s: %w", header.Name, err)
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("restore parent for %s: %w", header.Name, err)
			}
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("restore symlink %s: %w", header.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("restore parent for %s: %w", header.Name, err)
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
			if err != nil {
				return fmt.Errorf("restore file %s: %w", header.Name, err)
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				_ = file.Close()
				return fmt.Errorf("write restored file %s: %w", header.Name, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close restored file %s: %w", header.Name, err)
			}
			_ = os.Chtimes(targetPath, header.ModTime, header.ModTime)
		default:
			return fmt.Errorf("unsupported archive entry %s", header.Name)
		}
	}
	return nil
}

func safeJoin(root string, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("unsafe absolute archive path %q", name)
	}
	cleanName := filepath.Clean(name)
	if cleanName == "." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) || cleanName == ".." {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	target := filepath.Join(root, cleanName)
	cleanRoot := filepath.Clean(root)
	if target != cleanRoot && !strings.HasPrefix(target, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return target, nil
}

func validateSymlinkTarget(root string, linkPath string, linkTarget string) error {
	if strings.TrimSpace(linkTarget) == "" {
		return fmt.Errorf("empty symlink target")
	}
	if filepath.IsAbs(linkTarget) {
		return fmt.Errorf("unsafe absolute symlink target %q", linkTarget)
	}
	cleanRoot := filepath.Clean(root)
	resolved := filepath.Clean(filepath.Join(filepath.Dir(linkPath), linkTarget))
	if resolved != cleanRoot && !strings.HasPrefix(resolved, cleanRoot+string(os.PathSeparator)) {
		return fmt.Errorf("unsafe symlink target %q", linkTarget)
	}
	return nil
}

func readManifestFile(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest.json: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest.json: %w", err)
	}
	return manifest, nil
}

func validateConfigKey(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read required archive entry data/config.key: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("required archive entry data/config.key is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("required archive entry data/config.key mode is %04o; restore requires it to be unreadable by group and other", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read required archive entry data/config.key: %w", err)
	}
	if len(data) != 32 {
		return fmt.Errorf("required archive entry data/config.key is %d bytes, want 32", len(data))
	}
	return nil
}

func validateRestoreTargetPaths(cfg config.Config) error {
	paths := []string{
		filepath.Dir(cfg.DataDir),
		cfg.DataDir,
		cfg.AppsDir,
		filepath.Dir(cfg.SQLiteDBPath),
		filepath.Dir(cfg.ConfigKeyPath),
		cfg.GitHomeDir,
		filepath.Dir(cfg.GitAuthorizedKeysPath),
		filepath.Dir(cfg.OperatorHostKeyPath),
		filepath.Dir(cfg.OperatorAuthorizedKeysPath),
		filepath.Dir(cfg.CaddyConfigPath),
		filepath.Dir(cfg.CaddyMainConfigPath),
	}
	seen := map[string]bool{}
	for _, path := range paths {
		cleanPath := filepath.Clean(path)
		if seen[cleanPath] {
			continue
		}
		seen[cleanPath] = true
		info, err := os.Stat(cleanPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("validate restore target %s: %w", cleanPath, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("restore target %s is not a directory", cleanPath)
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("restore target %s mode is %04o; fix ownership and permissions before restore", cleanPath, info.Mode().Perm())
		}
	}
	return nil
}

func restoreOptionalFile(sourcePath string, targetPath string) error {
	info, err := os.Stat(sourcePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read restored file %s: %w", sourcePath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("restored file %s is not a regular file", sourcePath)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create restore target directory: %w", err)
	}
	input, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("restore %s: %w", targetPath, err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return fmt.Errorf("restore %s: %w", targetPath, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("restore %s: %w", targetPath, err)
	}
	return nil
}

func requireRegularFile(path string, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", label)
	}
	return nil
}

func sourcePaths(cfg config.Config) SourcePaths {
	return SourcePaths{
		DataDir:             cfg.DataDir,
		SQLiteDBPath:        cfg.SQLiteDBPath,
		ConfigKeyPath:       cfg.ConfigKeyPath,
		GitAuthorizedKeys:   cfg.GitAuthorizedKeysPath,
		OperatorHostKey:     cfg.OperatorHostKeyPath,
		OperatorKeys:        cfg.OperatorAuthorizedKeysPath,
		CaddyConfigPath:     cfg.CaddyConfigPath,
		CaddyMainConfigPath: cfg.CaddyMainConfigPath,
	}
}

func defaultRestoreGuardrails() []string {
	return []string{
		"Stop sshdockd before restoring.",
		"Run restore as a user that can preserve SSHDock state ownership and file modes.",
		"Restore only onto a compatible single-node SSHDock install.",
		"Run sudo sshdock diagnostics after restore.",
	}
}

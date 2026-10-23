package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestVolumeArchiveRoundTripPreservesContentNumericOwnershipAndOrdinaryModes(t *testing.T) {
	directory := t.TempDir()
	manifest := volumeArchiveTestManifest()
	longName := strings.Repeat("long-", 30) + "/" + strings.Repeat("nested-", 30) + "/file"
	entries := []volumeArchiveTestEntry{
		{Header: tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o750, Uid: 1234, Gid: 5678}},
		{Header: tar.Header{Name: "./empty/", Typeflag: tar.TypeDir, Mode: 0, Uid: 42, Gid: 43}},
		{Header: tar.Header{Name: "./nested/content.txt", Mode: 0o640, Uid: 42, Gid: 43, Uname: "ignored-name", Gname: "ignored-group", ModTime: manifest.CreatedAt, PAXRecords: map[string]string{"atime": "1", "ctime": "2"}}, Data: []byte("private application data\x00\xff")},
		{Header: tar.Header{Name: "./nested/", Typeflag: tar.TypeDir, Mode: 0o710, Uid: 42, Gid: 43}},
		{Header: tar.Header{Name: "./" + longName, Mode: 0o700, Uid: 4_000_000_000, Gid: 123}, Data: []byte("long path")},
	}
	native := writeVolumeNativeTestTar(t, directory, entries)
	archive := filepath.Join(directory, "volume.tar.gz")
	created, err := packVolumeArchive(context.Background(), native, archive, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if created.Path != archive || created.Manifest != manifest || created.FileCount != 2 || created.DirectoryCount != 3 || created.TotalBytes != int64(len(entries[2].Data)+len(entries[4].Data)) {
		t.Fatalf("created inspection = %#v", created)
	}
	if info, err := os.Stat(archive); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("archive permissions: info=%v error=%v", info, err)
	}

	// Inspection validates every payload byte without depending on writable scratch space.
	t.Setenv("TMPDIR", filepath.Join(directory, "nonexistent-scratch-directory"))
	inspected, err := InspectVolume(context.Background(), archive)
	if err != nil || inspected != created {
		t.Fatalf("inspection = %#v, error=%v; want %#v", inspected, err, created)
	}
	t.Setenv("TMPDIR", directory)
	stage, err := stageVolumeArchive(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	if stage.Inspection != created {
		t.Fatalf("staged inspection = %#v, want %#v", stage.Inspection, created)
	}
	stagedPath := stage.TarPath
	if info, err := os.Stat(stagedPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stage permissions: info=%v error=%v", info, err)
	}
	file, err := os.Open(stagedPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := tar.NewReader(file)
	for i, want := range entries {
		header, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimPrefix(want.Header.Name, "./")
		if i == 0 {
			name = "./"
		}
		if header.Name != name || header.Mode != want.Header.Mode || header.Uid != want.Header.Uid || header.Gid != want.Header.Gid || header.Uname != "" || header.Gname != "" || header.Linkname != "" || len(header.Xattrs) != 0 {
			t.Fatalf("normalized header = %#v; want ordinary metadata for %#v", header, want.Header)
		}
		if header.PAXRecords["atime"] != "" || header.PAXRecords["ctime"] != "" {
			t.Fatalf("unsupported time metadata retained: %#v", header.PAXRecords)
		}
		data, err := io.ReadAll(reader)
		if err != nil || !bytes.Equal(data, want.Data) {
			t.Fatalf("entry bytes = %q, error=%v; want %q", data, err, want.Data)
		}
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatalf("extra staged data: %v", err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stagedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged data was not removed: %v", err)
	}
	if err := stage.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestVolumeArchiveRejectsUnsafeEntriesBeforeReturningStage(t *testing.T) {
	regular := func(name string) volumeArchiveTestEntry {
		return volumeArchiveTestEntry{Header: tar.Header{Name: name, Mode: 0o600}, Data: []byte("payload")}
	}
	withHeader := func(header tar.Header) []volumeArchiveTestEntry {
		return []volumeArchiveTestEntry{{Header: header}}
	}
	cases := []struct {
		name    string
		entries []volumeArchiveTestEntry
	}{
		{"absolute path", []volumeArchiveTestEntry{regular("/outside")}},
		{"traversal", []volumeArchiveTestEntry{regular("data/../outside")}},
		{"nested traversal", []volumeArchiveTestEntry{regular("data/a/../../outside")}},
		{"dot component", []volumeArchiveTestEntry{regular("data/a/./file")}},
		{"empty component", []volumeArchiveTestEntry{regular("data/a//file")}},
		{"backslash", []volumeArchiveTestEntry{regular(`data/..\outside`)}},
		{"outside data root", []volumeArchiveTestEntry{regular("other/file")}},
		{"oversized path", []volumeArchiveTestEntry{regular("data/" + strings.Repeat("x", maxVolumePathBytes+1))}},
		{"duplicate path", []volumeArchiveTestEntry{regular("data/file"), regular("data/file")}},
		{"duplicate alias", []volumeArchiveTestEntry{regular("data/file"), regular("./data/file")}},
		{"file ancestor first", []volumeArchiveTestEntry{regular("data/a"), regular("data/a/file")}},
		{"file ancestor later", []volumeArchiveTestEntry{regular("data/a/file"), regular("data/a")}},
		{"duplicate root", withHeader(tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o755})},
		{"symlink", withHeader(tar.Header{Name: "data/link", Typeflag: tar.TypeSymlink, Linkname: "file", Mode: 0o777})},
		{"hardlink", withHeader(tar.Header{Name: "data/link", Typeflag: tar.TypeLink, Linkname: "data/file", Mode: 0o600})},
		{"FIFO", withHeader(tar.Header{Name: "data/pipe", Typeflag: tar.TypeFifo, Mode: 0o600})},
		{"character device", withHeader(tar.Header{Name: "data/device", Typeflag: tar.TypeChar, Mode: 0o600, Devmajor: 1, Devminor: 3})},
		{"block device", withHeader(tar.Header{Name: "data/device", Typeflag: tar.TypeBlock, Mode: 0o600, Devmajor: 1, Devminor: 3})},
		{"contiguous file", withHeader(tar.Header{Name: "data/special", Typeflag: tar.TypeCont, Mode: 0o600})},
		{"setuid", withHeader(tar.Header{Name: "data/file", Mode: 0o4600})},
		{"setgid", withHeader(tar.Header{Name: "data/file", Mode: 0o2600})},
		{"sticky", withHeader(tar.Header{Name: "data/dir/", Typeflag: tar.TypeDir, Mode: 0o1700})},
		{"negative UID", withHeader(tar.Header{Name: "data/file", Mode: 0o600, Uid: -1})},
		{"negative GID", withHeader(tar.Header{Name: "data/file", Mode: 0o600, Gid: -1})},
		{"invalid UID sentinel", withHeader(tar.Header{Name: "data/file", Mode: 0o600, Uid: 4_294_967_295})},
		{"ACL", []volumeArchiveTestEntry{{Header: tar.Header{Name: "data/file", Mode: 0o600}, RawPAX: map[string]string{"SCHILY.acl.access": "user::rw-,user:1000:r--,group::---,mask::r--,other::---"}}}},
		{"xattr", []volumeArchiveTestEntry{{Header: tar.Header{Name: "data/file", Mode: 0o600}, RawPAX: map[string]string{"SCHILY.xattr.user.secret": "private"}}}},
		{"SELinux", []volumeArchiveTestEntry{{Header: tar.Header{Name: "data/file", Mode: 0o600}, RawPAX: map[string]string{"RHT.security.selinux": "system_u:object_r:container_file_t:s0"}}}},
		{"unknown metadata", []volumeArchiveTestEntry{{Header: tar.Header{Name: "data/file", Mode: 0o600}, RawPAX: map[string]string{"vendor.unknown": "value"}}}},
		{"sparse PAX", []volumeArchiveTestEntry{{Header: tar.Header{Name: "data/file", Mode: 0o600}, Data: []byte("x"), RawPAX: map[string]string{"GNU.sparse.map": "0,1", "GNU.sparse.size": "1", "GNU.sparse.numblocks": "1"}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv("TMPDIR", directory)
			entries := append([]volumeArchiveTestEntry{{Header: tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o750}}}, test.entries...)
			archive := writeVolumeTestArchive(t, directory, nil, entries, nil, nil)
			_, inspectErr := InspectVolume(context.Background(), archive)
			if inspectErr == nil {
				t.Fatal("unsafe archive passed inspection")
			}
			if len(test.entries) == 1 && len(test.entries[0].RawPAX) > 0 && !strings.Contains(inspectErr.Error(), "unsupported volume metadata") && !strings.Contains(inspectErr.Error(), "unsupported link, device or extended metadata") {
				t.Fatalf("PAX fixture failed before the supported-metadata boundary: %v", inspectErr)
			}
			if staged, err := stageVolumeArchive(context.Background(), archive); err == nil {
				_ = staged.Close()
				t.Fatal("unsafe archive returned a restore stage")
			}
			assertNoVolumeArchiveTemps(t, directory)
		})
	}
}

func TestVolumeArchiveRequiresCompleteTarAndGzipIntegrity(t *testing.T) {
	entries := []volumeArchiveTestEntry{
		{Header: tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o750}},
		{Header: tar.Header{Name: "data/file", Mode: 0o600}, Data: []byte("payload")},
	}
	cases := []struct {
		name     string
		tarEdit  func([]byte) []byte
		gzipEdit func([]byte) []byte
	}{
		{"missing tar end", func(data []byte) []byte { return data[:len(data)-1024] }, nil},
		{"truncated payload", func(data []byte) []byte { return data[:len(data)-1024-512+2] }, nil},
		{"hidden tar content", func(data []byte) []byte { return append(data, []byte("hidden")...) }, nil},
		{"bad tar checksum", func(data []byte) []byte { data[148] ^= 1; return data }, nil},
		{"bad gzip footer checksum", nil, func(data []byte) []byte { data[len(data)-8] ^= 1; return data }},
		{"bad gzip footer size", nil, func(data []byte) []byte { data[len(data)-1] ^= 1; return data }},
		{"missing gzip footer", nil, func(data []byte) []byte { return data[:len(data)-8] }},
		{"truncated gzip footer", nil, func(data []byte) []byte { return data[:len(data)-1] }},
		{"concatenated gzip members", nil, func(data []byte) []byte { return append(data, data...) }},
		{"trailing compressed data", nil, func(data []byte) []byte { return append(data, 0) }},
		{"excessive metadata padding", func(data []byte) []byte { return append(data, make([]byte, maxVolumeMetadataBytes)...) }, nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv("TMPDIR", directory)
			archive := writeVolumeTestArchive(t, directory, nil, entries, test.tarEdit, test.gzipEdit)
			if _, err := InspectVolume(context.Background(), archive); err == nil {
				t.Fatal("corrupt archive passed complete inspection")
			}
			if stage, err := stageVolumeArchive(context.Background(), archive); err == nil {
				_ = stage.Close()
				t.Fatal("corrupt archive returned a restore stage")
			}
			assertNoVolumeArchiveTemps(t, directory)
		})
	}
}

func TestVolumeArchiveRejectsInvalidAndUnboundedManifest(t *testing.T) {
	manifest := volumeArchiveTestManifest()
	valid, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"unknown format":  []byte(strings.Replace(string(valid), VolumeFormatVersion, "unknown/v2", 1)),
		"unknown field":   []byte(strings.TrimSuffix(string(valid), "}") + `,"extra":"unsupported"}`),
		"multiple values": append(slices.Clone(valid), []byte(` {}`)...),
		"missing time":    []byte(strings.Replace(string(valid), manifest.CreatedAt.Format(time.RFC3339), "0001-01-01T00:00:00Z", 1)),
		"unsafe source":   []byte(strings.Replace(string(valid), "source_data", "../source", 1)),
		"custom driver":   []byte(strings.Replace(string(valid), `"local"`, `"nfs"`, 1)),
		"oversized":       bytes.Repeat([]byte(" "), maxVolumeManifestBytes+1),
		"empty":           {},
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			archive := writeVolumeTestArchive(t, directory, data, []volumeArchiveTestEntry{{Header: tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o750}}}, nil, nil)
			if _, err := InspectVolume(context.Background(), archive); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestVolumeArchiveBoundsInferredAncestorMetadata(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("TMPDIR", directory)
	entries := []volumeArchiveTestEntry{{Header: tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o750}}}
	// Short file bodies must not permit an unbounded map of inferred ancestors.
	// Every individual path is valid and under the per-path limit.
	for i := range 5 {
		name := fmt.Sprintf("data/root%d/", i) + strings.Repeat("a/", 2040) + "file"
		entries = append(entries, volumeArchiveTestEntry{Header: tar.Header{Name: name, Mode: 0o600}})
	}
	archive := writeVolumeTestArchive(t, directory, nil, entries, nil, nil)
	if _, err := InspectVolume(context.Background(), archive); err == nil || !strings.Contains(err.Error(), "combined path metadata") {
		t.Fatalf("aggregate metadata error = %v", err)
	}
	if stage, err := stageVolumeArchive(context.Background(), archive); err == nil {
		_ = stage.Close()
		t.Fatal("aggregate metadata overflow returned a stage")
	}
	assertNoVolumeArchiveTemps(t, directory)
}

func TestVolumeArchiveRequiresOneManifestAndDataRoot(t *testing.T) {
	for _, entries := range [][]volumeArchiveTestEntry{
		{{Header: tar.Header{Name: "data/file", Mode: 0o600}, Data: []byte("root directory omitted")}},
		{{Header: tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o750}}, {Header: tar.Header{Name: "manifest.json", Mode: 0o600}, Data: []byte(`{}`)}},
	} {
		directory := t.TempDir()
		archive := writeVolumeTestArchive(t, directory, nil, entries, nil, nil)
		if _, err := InspectVolume(context.Background(), archive); err == nil {
			t.Fatal("invalid archive layout accepted")
		}
	}
}

func TestVolumeArchivePublicationNeverReplacesExistingDestination(t *testing.T) {
	directory := t.TempDir()
	native := writeVolumeNativeTestTar(t, directory, []volumeArchiveTestEntry{{Header: tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o750}}})
	protected := filepath.Join(directory, "existing")
	if err := os.WriteFile(protected, []byte("keep"), 0o640); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{protected, filepath.Join(directory, "symlink.tar.gz")} {
		if destination != protected {
			if err := os.Symlink(protected, destination); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := packVolumeArchive(context.Background(), native, destination, volumeArchiveTestManifest()); err == nil {
			t.Fatal("existing destination was replaced")
		}
		if content, err := os.ReadFile(protected); err != nil || string(content) != "keep" {
			t.Fatalf("existing data changed: %q %v", content, err)
		}
		assertNoVolumeArchiveTemps(t, directory)
	}
}

func TestVolumeArchiveCancellationDuringPayloadRemovesPrivateStages(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("TMPDIR", directory)
	entries := []volumeArchiveTestEntry{
		{Header: tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o750}},
		{Header: tar.Header{Name: "./large", Mode: 0o600}, Data: bytes.Repeat([]byte("sensitive data"), 100_000)},
	}
	native := writeVolumeNativeTestTar(t, directory, entries)
	archive := filepath.Join(directory, "archive.tar.gz")
	if _, err := packVolumeArchive(context.Background(), native, archive, volumeArchiveTestManifest()); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"pack", "inspect", "stage"} {
		t.Run(operation, func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &volumeArchiveCancelContext{Context: base, cancel: cancel, remaining: 10}
			var err error
			switch operation {
			case "pack":
				destination := filepath.Join(directory, "cancelled.tar.gz")
				_, err = packVolumeArchive(ctx, native, destination, volumeArchiveTestManifest())
				if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("cancelled archive published: %v", statErr)
				}
			case "inspect":
				_, err = InspectVolume(ctx, archive)
			case "stage":
				var stage *stagedVolumeArchive
				stage, err = stageVolumeArchive(ctx, archive)
				if stage != nil {
					_ = stage.Close()
					t.Fatal("cancelled restore returned a stage")
				}
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error = %v", err)
			}
			assertNoVolumeArchiveTemps(t, directory)
		})
	}
}

func TestVolumePackRejectsUnsupportedNativeDataAndCleansTemporaryArchive(t *testing.T) {
	for _, entry := range []volumeArchiveTestEntry{
		{Header: tar.Header{Name: "./link", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "file"}},
		{Header: tar.Header{Name: "../outside", Mode: 0o600}},
		{Header: tar.Header{Name: "./mode", Mode: 0o4600}},
		{Header: tar.Header{Name: "./xattr", Mode: 0o600}, RawPAX: map[string]string{"SCHILY.xattr.user.test": "value"}},
	} {
		t.Run(entry.Header.Name, func(t *testing.T) {
			directory := t.TempDir()
			native := writeVolumeNativeTestTar(t, directory, []volumeArchiveTestEntry{{Header: tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o750}}, entry})
			destination := filepath.Join(directory, "rejected.tar.gz")
			if _, err := packVolumeArchive(context.Background(), native, destination, volumeArchiveTestManifest()); err == nil {
				t.Fatal("unsupported source was packed")
			}
			if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected source published archive: %v", err)
			}
			assertNoVolumeArchiveTemps(t, directory)
		})
	}
}

type volumeArchiveTestEntry struct {
	Header tar.Header
	Data   []byte
	RawPAX map[string]string
}

func volumeArchiveTestManifest() VolumeManifest {
	return VolumeManifest{FormatVersion: VolumeFormatVersion, CreatedAt: time.Date(2026, 10, 23, 1, 2, 3, 0, time.UTC), SourceVolume: "source_data", Driver: "local"}
}

func writeVolumeNativeTestTar(t *testing.T, directory string, entries []volumeArchiveTestEntry) string {
	t.Helper()
	var raw bytes.Buffer
	writer := tar.NewWriter(&raw)
	writeVolumeTestEntries(t, &raw, writer, entries)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "native.tar")
	if err := os.WriteFile(file, raw.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func writeVolumeTestArchive(t *testing.T, directory string, manifest []byte, entries []volumeArchiveTestEntry, tarEdit, gzipEdit func([]byte) []byte) string {
	t.Helper()
	if manifest == nil {
		var err error
		manifest, err = json.Marshal(volumeArchiveTestManifest())
		if err != nil {
			t.Fatal(err)
		}
	}
	var raw bytes.Buffer
	writer := tar.NewWriter(&raw)
	writeVolumeTestEntries(t, &raw, writer, append([]volumeArchiveTestEntry{{Header: tar.Header{Name: "manifest.json", Mode: 0o600}, Data: manifest}}, entries...))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data := raw.Bytes()
	if tarEdit != nil {
		data = tarEdit(data)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	data = compressed.Bytes()
	if gzipEdit != nil {
		data = gzipEdit(data)
	}
	file := filepath.Join(directory, "archive.tar.gz")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func writeVolumeTestEntries(t *testing.T, raw *bytes.Buffer, writer *tar.Writer, entries []volumeArchiveTestEntry) {
	t.Helper()
	for _, entry := range entries {
		if len(entry.RawPAX) > 0 {
			if err := writer.Flush(); err != nil {
				t.Fatal(err)
			}
			// Go's tar writer omits GNU sparse keys, so write a real PAX header
			// directly to exercise metadata emitted by the GNU tar source helper.
			raw.Write(volumeArchiveRawPAX(t, entry.RawPAX))
		}
		header := entry.Header
		header.Format = tar.FormatPAX
		header.Size = int64(len(entry.Data))
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(entry.Data); err != nil {
			t.Fatal(err)
		}
	}
}

func volumeArchiveRawPAX(t *testing.T, records map[string]string) []byte {
	t.Helper()
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var body strings.Builder
	for _, key := range keys {
		text := key + "=" + records[key] + "\n"
		length := len(text) + 2
		for {
			actual := len(fmt.Sprint(length)) + 1 + len(text)
			if actual == length {
				break
			}
			length = actual
		}
		fmt.Fprintf(&body, "%d %s", length, text)
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: "PaxHeaders/entry", Mode: 0o600, Size: int64(body.Len()), Format: tar.FormatUSTAR}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, body.String()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data := buffer.Bytes()[:512+((body.Len()+511)/512)*512]
	data[156] = tar.TypeXHeader
	for i := 148; i < 156; i++ {
		data[i] = ' '
	}
	checksum := 0
	for _, value := range data[:512] {
		checksum += int(value)
	}
	copy(data[148:156], fmt.Sprintf("%06o\x00 ", checksum))
	return data
}

func assertNoVolumeArchiveTemps(t *testing.T, directory string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(directory, ".sshdock-volume-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("private volume stages remain: %v, error=%v", matches, err)
	}
}

type volumeArchiveCancelContext struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *volumeArchiveCancelContext) Err() error {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

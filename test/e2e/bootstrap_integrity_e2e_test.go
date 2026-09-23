//go:build e2e

package e2e

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBootstrapValidatesReleaseBeforeReplacingEitherBinary(t *testing.T) {
	for _, failure := range []string{"none", "checksum", "missing daemon", "invalid daemon", "copy daemon"} {
		t.Run(failure, func(t *testing.T) {
			tmp := t.TempDir()
			fakeBin, fixture, installRoot := filepath.Join(tmp, "fake-bin"), filepath.Join(tmp, "release"), filepath.Join(tmp, "root")
			writeBootstrapFakeCommands(t, fakeBin)
			if err := os.MkdirAll(fixture, 0o755); err != nil {
				t.Fatal(err)
			}
			asset := "sshdock_v1-test_linux_" + runtime.GOARCH + ".tar.gz"
			archive, err := os.Create(filepath.Join(fixture, asset))
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(archive)
			tw := tar.NewWriter(gz)
			for _, bin := range []string{"sshdock", "sshdockd"} {
				if failure == "missing daemon" && bin == "sshdockd" {
					continue
				}
				content := "#!/bin/sh\nprintf 'new binary\\n'\n"
				if failure == "invalid daemon" && bin == "sshdockd" {
					content = "#!/bin/sh\nexit 1\n"
				}
				if err := tw.WriteHeader(&tar.Header{Name: bin, Mode: 0o755, Size: int64(len(content))}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write([]byte(content)); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(fixture, asset))
			if err != nil {
				t.Fatal(err)
			}
			checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(data), asset)
			if failure == "checksum" {
				checksum = strings.Repeat("0", 64) + "  " + asset + "\n"
			}
			if err := os.WriteFile(filepath.Join(fixture, asset+".sha256"), []byte(checksum), 0o644); err != nil {
				t.Fatal(err)
			}
			writeFakeCommand(t, fakeBin, "curl", "#!/bin/sh\ncp \"$RELEASE_FIXTURE/${2##*/}\" \"$4\"\n")
			if _, err := exec.LookPath("sha256sum"); err != nil {
				requireCommandOrSkip(t, "shasum")
				writeFakeCommand(t, fakeBin, "sha256sum", "#!/bin/sh\nexec shasum -a 256 \"$@\"\n")
			}
			if failure == "copy daemon" {
				cp, err := exec.LookPath("cp")
				if err != nil {
					t.Fatal(err)
				}
				writeFakeCommand(t, fakeBin, "cp", "#!/bin/sh\ncase \"$1\" in */sshdockd) exit 1 ;; esac\nexec "+cp+" \"$@\"\n")
			}
			binDir := filepath.Join(installRoot, "usr/local/bin")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, bin := range []string{"sshdock", "sshdockd"} {
				if err := os.WriteFile(filepath.Join(binDir, bin), []byte("previous "+bin), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command("bash", "scripts/bootstrap.sh")
			command.Dir = filepath.Join("..", "..")
			logPath := filepath.Join(tmp, "commands.log")
			command.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"), "RELEASE_FIXTURE="+fixture,
				"SSHDOCK_TAG=v1-test", "SSHDOCK_BOOTSTRAP_ROOT="+installRoot, "SSHDOCK_BOOTSTRAP_SOURCE_BIN_DIR=", "SSHDOCK_RELEASE_BASE_URL=https://releases.example.com",
				"SSHDOCK_BOOTSTRAP_INSTALL_DEPS=0", "SSHDOCK_BOOTSTRAP_SKIP_USER=1", "SSHDOCK_BOOTSTRAP_SKIP_CHOWN=1", "SSHDOCK_BOOTSTRAP_FAKE_LOG="+logPath)
			output, err := command.CombinedOutput()
			if (err == nil) != (failure == "none") {
				t.Fatalf("bootstrap result=%v:\n%s", err, output)
			}
			for _, bin := range []string{"sshdock", "sshdockd"} {
				got := readFile(t, filepath.Join(binDir, bin))
				if failure == "none" {
					if !strings.Contains(got, "new binary") {
						t.Fatalf("%s not installed: %q", bin, got)
					}
				} else if got != "previous "+bin {
					t.Fatalf("failed upgrade replaced %s: %q", bin, got)
				}
			}
			if failure != "none" && strings.Contains(readFile(t, logPath), "systemctl restart") {
				t.Fatal("failed upgrade restarted services")
			}
			if paths, err := filepath.Glob(filepath.Join(binDir, ".sshdock-install.*")); err != nil || len(paths) != 0 {
				t.Fatalf("staging files remain: %v %v", paths, err)
			}
		})
	}
}

package capture

import (
	"bytes"
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadableForegroundRepairsInvisibleCapturedText(t *testing.T) {
	black := color.RGBA{A: 255}
	got := readableForeground(black, black)
	if got == black {
		t.Fatalf("readableForeground = %#v, want contrasting text", got)
	}

	blue := color.RGBA{B: 255, A: 255}
	if got := readableForeground(blue, black); got != blue {
		t.Fatalf("readableForeground changed visible color to %#v", got)
	}

	nearBlack := color.RGBA{R: 8, G: 12, B: 18, A: 255}
	if got := readableForeground(nearBlack, black); got == nearBlack {
		t.Fatalf("readableForeground kept low-contrast color %#v", got)
	}

	darkBlue := color.RGBA{B: 128, A: 255}
	if got := readableForeground(darkBlue, black); got == darkBlue {
		t.Fatalf("readableForeground kept dark text %#v on black", got)
	}
}

func TestTerminalCaptureReplaysAlternateScreenAndCursorMoves(t *testing.T) {
	term := NewTerminal(6, 24, nil)
	stream := []byte("\x1b[?1049h\x1b[HHello\x1b[2;4HWorld\x1b[3;1H\x1b[35mTabs")
	if _, err := term.Write(stream); err != nil {
		t.Fatalf("Write: %v", err)
	}

	screen := term.Screen()
	text := screen.Text()

	for _, want := range []string{"Hello", "   World", "Tabs"} {
		if !strings.Contains(text, want) {
			t.Fatalf("screen text missing %q:\n%s", want, text)
		}
	}
	if screen.Rows != 6 || screen.Cols != 24 {
		t.Fatalf("screen size = %dx%d, want 6x24", screen.Rows, screen.Cols)
	}
	if got := screen.Cells[2][0].FG; got.R == 0 && got.B == 0 {
		t.Fatalf("expected SGR color to be captured, got %#v", got)
	}
}

func TestTerminalCaptureWritesTerminalResponses(t *testing.T) {
	var responses bytes.Buffer
	term := NewTerminal(7, 19, &responses)

	if _, err := term.Write([]byte("\x1b[4;6H\x1b[6n\x1b]11;?\a")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := responses.String()
	if !strings.Contains(got, "\x1b[4;6R") {
		t.Fatalf("response missing cursor report: %q", got)
	}
	if !strings.Contains(got, "\x1b]11;rgb:") {
		t.Fatalf("response missing background color report: %q", got)
	}
}

func TestWriteArtifactsPersistsRawTextPNGAndManifest(t *testing.T) {
	term := NewTerminal(4, 20, nil)
	raw := []byte("\x1b[HReal SSH Dashboard\x1b[2;1Hdashboard-app")
	if _, err := term.Write(raw); err != nil {
		t.Fatalf("Write: %v", err)
	}
	dir := t.TempDir()

	manifest, err := WriteArtifacts(ArtifactSet{
		Dir: dir,
		Raw: raw,
		Frames: []Frame{
			{Name: "summary", Screen: term.Screen()},
		},
		Rows:    4,
		Cols:    20,
		Command: []string{"ssh", "-tt", "dashboard@127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("WriteArtifacts: %v", err)
	}

	for _, rel := range []string{"session.ansi", "summary.txt", "summary.png", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("expected artifact %s: %v", rel, err)
		}
	}
	text := mustReadFile(t, filepath.Join(dir, "summary.txt"))
	if !strings.Contains(text, "Real SSH Dashboard") || !strings.Contains(text, "dashboard-app") {
		t.Fatalf("summary text missing dashboard content:\n%s", text)
	}
	pngBytes := mustReadBytes(t, filepath.Join(dir, "summary.png"))
	if !bytes.HasPrefix(pngBytes, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatalf("summary.png is not a PNG")
	}
	manifestBytes := mustReadBytes(t, filepath.Join(dir, "manifest.json"))
	var decoded Manifest
	if err := json.Unmarshal(manifestBytes, &decoded); err != nil {
		t.Fatalf("manifest JSON: %v", err)
	}
	if decoded.Rows != 4 || decoded.Cols != 20 {
		t.Fatalf("manifest size = %dx%d, want 4x20", decoded.Rows, decoded.Cols)
	}
	if len(decoded.Frames) != 1 || decoded.Frames[0].Name != "summary" {
		t.Fatalf("manifest frames = %#v", decoded.Frames)
	}
	if manifest.Frames[0].PNGPath != "summary.png" || manifest.RawANSIPath != "session.ansi" {
		t.Fatalf("returned manifest paths = %#v", manifest)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	return string(mustReadBytes(t, path))
}

func mustReadBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return b
}

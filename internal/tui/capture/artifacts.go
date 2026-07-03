package capture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Frame struct {
	Name   string
	Screen Screen
}

type ArtifactSet struct {
	Dir     string
	Raw     []byte
	Frames  []Frame
	Rows    int
	Cols    int
	Command []string
}

type Manifest struct {
	GeneratedAt string          `json:"generated_at"`
	Rows        int             `json:"rows"`
	Cols        int             `json:"cols"`
	Command     []string        `json:"command,omitempty"`
	RawANSIPath string          `json:"raw_ansi_path"`
	Frames      []ManifestFrame `json:"frames"`
}

type ManifestFrame struct {
	Name     string `json:"name"`
	TextPath string `json:"text_path"`
	PNGPath  string `json:"png_path"`
}

var unsafeFrameName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func WriteArtifacts(set ArtifactSet) (Manifest, error) {
	if strings.TrimSpace(set.Dir) == "" {
		return Manifest{}, fmt.Errorf("artifact dir is required")
	}
	if len(set.Frames) == 0 {
		return Manifest{}, fmt.Errorf("at least one frame is required")
	}
	if err := os.MkdirAll(set.Dir, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("create artifact dir: %w", err)
	}

	manifest := Manifest{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Rows:        set.Rows,
		Cols:        set.Cols,
		Command:     append([]string(nil), set.Command...),
		RawANSIPath: "session.ansi",
		Frames:      make([]ManifestFrame, 0, len(set.Frames)),
	}
	if err := os.WriteFile(filepath.Join(set.Dir, manifest.RawANSIPath), set.Raw, 0o644); err != nil {
		return Manifest{}, fmt.Errorf("write raw ANSI stream: %w", err)
	}

	for _, frame := range set.Frames {
		name := safeFrameName(frame.Name)
		textPath := name + ".txt"
		pngPath := name + ".png"
		if err := os.WriteFile(filepath.Join(set.Dir, textPath), []byte(frame.Screen.Text()+"\n"), 0o644); err != nil {
			return Manifest{}, fmt.Errorf("write %s: %w", textPath, err)
		}
		if err := WritePNG(filepath.Join(set.Dir, pngPath), frame.Screen); err != nil {
			return Manifest{}, fmt.Errorf("write %s: %w", pngPath, err)
		}
		manifest.Frames = append(manifest.Frames, ManifestFrame{
			Name:     frame.Name,
			TextPath: textPath,
			PNGPath:  pngPath,
		})
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, fmt.Errorf("marshal manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := os.WriteFile(filepath.Join(set.Dir, "manifest.json"), manifestBytes, 0o644); err != nil {
		return Manifest{}, fmt.Errorf("write manifest: %w", err)
	}
	return manifest, nil
}

func safeFrameName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "frame"
	}
	name = unsafeFrameName.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-.")
	if name == "" {
		return "frame"
	}
	return name
}

package capture

import (
	"fmt"
	"image/color"
	"io"
	"strings"

	headlessterm "github.com/danielgatis/go-headless-term"
)

// Terminal replays an ANSI terminal stream into an inspectable screen model.
type Terminal struct {
	term           *headlessterm.Terminal
	responseWriter io.Writer
}

// Cell is one visible terminal cell with resolved colors.
type Cell struct {
	Ch     rune
	FG     color.RGBA
	BG     color.RGBA
	Bold   bool
	Italic bool
	Dim    bool
}

// Screen is a fixed-size terminal frame captured from the active buffer.
type Screen struct {
	Rows  int
	Cols  int
	Cells [][]Cell
}

// NewTerminal creates a replay terminal. responseWriter receives terminal query
// responses, so a live PTY capture can satisfy Bubble Tea/termenv probes.
func NewTerminal(rows, cols int, responseWriter io.Writer) *Terminal {
	opts := []headlessterm.Option{headlessterm.WithSize(rows, cols)}
	if responseWriter != nil {
		opts = append(opts, headlessterm.WithPTYWriter(responseWriter))
	}
	return &Terminal{term: headlessterm.New(opts...), responseWriter: responseWriter}
}

func (t *Terminal) Write(p []byte) (int, error) {
	t.respondToDynamicColorQueries(p)
	return t.term.Write(p)
}

func (t *Terminal) Screen() Screen {
	rows := t.term.Rows()
	cols := t.term.Cols()
	screen := Screen{
		Rows:  rows,
		Cols:  cols,
		Cells: make([][]Cell, rows),
	}
	for row := 0; row < rows; row++ {
		screen.Cells[row] = make([]Cell, cols)
		for col := 0; col < cols; col++ {
			cell := t.term.Cell(row, col)
			screen.Cells[row][col] = screenCell(cell)
		}
	}
	return screen
}

func screenCell(cell *headlessterm.Cell) Cell {
	if cell == nil {
		return defaultCell()
	}
	ch := cell.Char
	if ch == 0 {
		ch = ' '
	}
	fg := headlessterm.ResolveDefaultColor(cell.Fg, true)
	bg := headlessterm.ResolveDefaultColor(cell.Bg, false)
	if cell.HasFlag(headlessterm.CellFlagReverse) {
		fg, bg = bg, fg
	}
	if cell.HasFlag(headlessterm.CellFlagHidden) {
		fg = bg
	}
	return Cell{
		Ch:     ch,
		FG:     fg,
		BG:     bg,
		Bold:   cell.HasFlag(headlessterm.CellFlagBold),
		Italic: cell.HasFlag(headlessterm.CellFlagItalic),
		Dim:    cell.HasFlag(headlessterm.CellFlagDim),
	}
}

func defaultCell() Cell {
	return Cell{
		Ch: ' ',
		FG: headlessterm.ResolveDefaultColor(nil, true),
		BG: headlessterm.ResolveDefaultColor(nil, false),
	}
}

func (t *Terminal) respondToDynamicColorQueries(p []byte) {
	if t.responseWriter == nil {
		return
	}
	for offset := 0; offset < len(p); offset++ {
		if p[offset] != 0x1b || offset+2 >= len(p) || p[offset+1] != ']' {
			continue
		}
		seqStart := offset + 2
		seqEnd := -1
		terminator := "\a"
		for cursor := seqStart; cursor < len(p); cursor++ {
			if p[cursor] == '\a' {
				seqEnd = cursor
				terminator = "\a"
				break
			}
			if p[cursor] == 0x1b && cursor+1 < len(p) && p[cursor+1] == '\\' {
				seqEnd = cursor
				terminator = "\x1b\\"
				break
			}
		}
		if seqEnd == -1 {
			continue
		}
		seq := string(p[seqStart:seqEnd])
		prefix, ok := dynamicColorQueryPrefix(seq)
		if !ok {
			continue
		}
		colorValue := dynamicColorValue(prefix)
		_, _ = io.WriteString(t.responseWriter, fmt.Sprintf("\x1b]%s;rgb:%02x/%02x/%02x%s", prefix, colorValue.R, colorValue.G, colorValue.B, terminator))
	}
}

func dynamicColorQueryPrefix(seq string) (string, bool) {
	for _, prefix := range []string{"10", "11", "12"} {
		if seq == prefix+";?" {
			return prefix, true
		}
	}
	return "", false
}

func dynamicColorValue(prefix string) color.RGBA {
	switch prefix {
	case "11":
		return headlessterm.ResolveDefaultColor(nil, false)
	case "12":
		return headlessterm.DefaultCursorColor
	default:
		return headlessterm.ResolveDefaultColor(nil, true)
	}
}

func (s Screen) Text() string {
	lines := make([]string, 0, s.Rows)
	for _, row := range s.Cells {
		var builder strings.Builder
		for _, cell := range row {
			ch := cell.Ch
			if ch == 0 {
				ch = ' '
			}
			builder.WriteRune(ch)
		}
		lines = append(lines, strings.TrimRight(builder.String(), " "))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

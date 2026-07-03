package capture

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	cellWidth  = 8
	cellHeight = 16
	textBase   = 13
)

func WritePNG(path string, screen Screen) error {
	width := maxInt(1, screen.Cols*cellWidth)
	height := maxInt(1, screen.Rows*cellHeight)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 8, G: 12, B: 18, A: 255}), image.Point{}, draw.Src)

	for row := 0; row < screen.Rows; row++ {
		for col := 0; col < screen.Cols; col++ {
			cell := screen.Cells[row][col]
			bg := cell.BG
			if bg.A == 0 {
				bg = color.RGBA{R: 8, G: 12, B: 18, A: 255}
			}
			rect := image.Rect(col*cellWidth, row*cellHeight, (col+1)*cellWidth, (row+1)*cellHeight)
			draw.Draw(img, rect, image.NewUniform(bg), image.Point{}, draw.Src)
		}
	}

	for row := 0; row < screen.Rows; row++ {
		for col := 0; col < screen.Cols; col++ {
			cell := screen.Cells[row][col]
			ch := printableRune(cell.Ch)
			if ch == ' ' {
				continue
			}
			fg := cell.FG
			if fg.A == 0 {
				fg = color.RGBA{R: 230, G: 235, B: 241, A: 255}
			}
			drawer := font.Drawer{
				Dst:  img,
				Src:  image.NewUniform(fg),
				Face: basicfont.Face7x13,
				Dot:  fixed.P(col*cellWidth, row*cellHeight+textBase),
			}
			drawer.DrawString(string(ch))
		}
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return png.Encode(file, img)
}

func printableRune(ch rune) rune {
	switch ch {
	case 0:
		return ' '
	case '┌', '┐', '└', '┘', '┬', '┴', '├', '┤', '┼':
		return '+'
	case '─', '━':
		return '-'
	case '│', '┃':
		return '|'
	default:
		if ch < 32 {
			return ' '
		}
		return ch
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

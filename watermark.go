package main

import (
	_ "embed"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
)

//go:embed assets/ralph.txt
var ralphArtwork string

var mainBackground = lipgloss.Color("#17141F")

const watermarkOpacity = 0.10 // Terminal colours are pre-blended; ANSI has no alpha channel.

func blendWatermark(fg color.Color) color.Color {
	r, g, b, _ := fg.RGBA()
	br, bg, bb, _ := mainBackground.RGBA()
	blend := func(front, back uint32) uint8 {
		return uint8(math.Round((float64(front>>8)*watermarkOpacity + float64(back>>8)*(1-watermarkOpacity))))
	}
	return color.RGBA{R: blend(r, br), G: blend(g, bg), B: blend(b, bb), A: 255}
}

func artworkColour(x, y int) color.Color {
	shade := "#F5D45C" // Skin.
	switch {
	case y >= 60:
		shade = "#95755E" // Shoes.
	case y >= 45 && y <= 51 && (x < 35 || x > 68):
		shade = "#F5D45C" // Hands beside the trousers.
	case y >= 45:
		shade = "#7C88B6" // Trousers.
	case y >= 42:
		shade = "#E66E78" // Belt.
	case y >= 29:
		shade = "#80C5F1" // Shirt.
	case y >= 17 && y <= 20 && ((x >= 43 && x <= 52) || (x >= 59 && x <= 67)):
		shade = "#F3EFDF" // Eyes.
	case y <= 15:
		shade = "#BCA064" // Hair.
	}
	return blendWatermark(lipgloss.Color(shade))
}

func watermark(width, height int) *lipgloss.Canvas {
	canvas := lipgloss.NewCanvas(width, height)
	if width < 1 || height < 1 {
		return canvas
	}
	lines := strings.Split(strings.TrimRight(ralphArtwork, "\n"), "\n")
	// Ignore the dotted backdrop and scattered colons, retaining the figure's interior.
	minX, minY, maxX, maxY := 10000, 10000, 0, 0
	for y, line := range lines {
		for x, ch := range line {
			if strings.ContainsRune("-=+*#%@", ch) {
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			}
		}
	}
	if minX > maxX {
		return canvas
	}
	sourceW, sourceH := maxX-minX+1, maxY-minY+1
	scale := min(1.0, float64(width)/float64(sourceW), float64(height)/float64(sourceH))
	w, h := max(1, int(float64(sourceW)*scale)), max(1, int(float64(sourceH)*scale))
	var art strings.Builder
	for y := 0; y < h; y++ {
		sy := minY + min(sourceH-1, int((float64(y)+0.5)*float64(sourceH)/float64(h)))
		line := lines[sy]
		left := strings.IndexAny(line, "-=+*#%@")
		right := strings.LastIndexAny(line, "-=+*#%@")
		for x := 0; x < w; x++ {
			sx := minX + min(sourceW-1, int((float64(x)+0.5)*float64(sourceW)/float64(w)))
			if sx < left || sx > right || sx >= len(line) || line[sx] == ' ' {
				art.WriteByte(' ')
				continue
			}
			art.WriteString(lipgloss.NewStyle().Foreground(artworkColour(sx, sy)).Render(string(line[sx])))
		}
		if y < h-1 {
			art.WriteByte('\n')
		}
	}
	return canvas.Compose(lipgloss.NewCompositor(lipgloss.NewLayer(art.String()).X((width - w) / 2).Y((height - h) / 2)))
}

func mainPanel(content string, width, height int) string {
	frame := panel.Width(width).Height(height).Render(content)
	w, h := lipgloss.Size(frame)
	foreground := lipgloss.NewCanvas(w, h).Compose(lipgloss.NewLayer(frame))
	art := watermark(max(0, w-4), max(0, h-2))
	for y := 1; y < h-1; y++ {
		// Keep complete text spans, including word spacing and wide glyphs, intact.
		textEnd := 1
		for x := 2; x < w-2; x++ {
			if cell := foreground.CellAt(x, y); cell != nil && cell.Content != " " {
				textEnd = x
			}
		}
		for x := textEnd + 1; x < w-2; x++ {
			cell := foreground.CellAt(x, y)
			if cell != nil && (cell.Content != " " || cell.Style.Bg != nil || cell.Link.URL != "") {
				continue
			}
			if mark := art.CellAt(x-2, y-1); mark != nil && mark.Content != " " && mark.Content != "" {
				foreground.SetCell(x, y, mark)
			}
		}
	}
	return foreground.Render()
}

// Package img decodes images and draws them in the terminal, either with
// Unicode half blocks (works in any truecolor terminal) or the kitty
// graphics protocol (kitty, WezTerm, Ghostty, lumen).
package img

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"

	"github.com/Vaspyyy/glint/internal/regfile"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

var exts = map[string]bool{"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "bmp": true, "tif": true, "tiff": true}

func Supported(ext string) bool { return exts[strings.ToLower(strings.TrimPrefix(ext, "."))] }

const maxDecodeBytes = 64 << 20

func Load(path string) (image.Image, error) {
	f, err := regfile.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > maxDecodeBytes {
		return nil, fmt.Errorf("too large to preview")
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if cfg.Width*cfg.Height > 50_000_000 {
		return nil, fmt.Errorf("too many pixels to preview")
	}
	f.Seek(0, 0)
	m, _, err := image.Decode(f)
	return m, err
}

func Dimensions(path string) (int, int, error) {
	f, err := regfile.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	return cfg.Width, cfg.Height, err
}

// Fit returns the largest w×h with src's aspect ratio inside maxW×maxH.
func Fit(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return maxW, maxH
	}
	w, h := maxW, srcH*maxW/srcW
	if h > maxH {
		h, w = maxH, srcW*maxH/srcH
	}
	return max(w, 1), max(h, 1)
}

// Resize is a box-filter downscale (and nearest-neighbour upscale).
func Resize(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	for y := range h {
		y0 := b.Min.Y + y*sh/h
		y1 := max(b.Min.Y+(y+1)*sh/h, y0+1)
		for x := range w {
			x0 := b.Min.X + x*sw/w
			x1 := max(b.Min.X+(x+1)*sw/w, x0+1)
			var r, g, bl, a, n uint64
			// sample at most 4×4 points per output pixel to stay fast on huge images
			sx := max((x1-x0)/4, 1)
			sy := max((y1-y0)/4, 1)
			for yy := y0; yy < y1; yy += sy {
				for xx := x0; xx < x1; xx += sx {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), uint8(a / n >> 8)})
		}
	}
	return dst
}

// HalfBlocks renders m into at most cols×rows cells using "▀" with a
// foreground (top pixel) and background (bottom pixel) color. Transparent
// pixels show the terminal background. Lines are not padded.
func HalfBlocks(m image.Image, cols, rows int) []string {
	b := m.Bounds()
	w, h := Fit(b.Dx(), b.Dy(), cols, rows*2)
	h += h % 2
	px := Resize(m, w, h)
	lines := make([]string, 0, h/2)
	for y := 0; y < h; y += 2 {
		var sb strings.Builder
		for x := range w {
			top := px.RGBAAt(x, y)
			bot := px.RGBAAt(x, y+1)
			switch {
			case top.A < 64 && bot.A < 64:
				sb.WriteString("\x1b[0m ")
			case bot.A < 64:
				fmt.Fprintf(&sb, "\x1b[0;38;2;%d;%d;%dm▀", top.R, top.G, top.B)
			case top.A < 64:
				fmt.Fprintf(&sb, "\x1b[0;38;2;%d;%d;%dm▄", bot.R, bot.G, bot.B)
			default:
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀", top.R, top.G, top.B, bot.R, bot.G, bot.B)
			}
		}
		sb.WriteString("\x1b[0m")
		lines = append(lines, sb.String())
	}
	return lines
}

// KittySupported guesses whether the terminal speaks the kitty graphics protocol.
func KittySupported() bool {
	switch {
	case os.Getenv("KITTY_WINDOW_ID") != "", os.Getenv("TERM") == "xterm-kitty", os.Getenv("TERM") == "xterm-ghostty":
		return true
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "WezTerm", "ghostty", "lumen":
		return true
	}
	return false
}

// CellsFor picks a cols×rows box for an image so it keeps its aspect ratio,
// assuming terminal cells are about twice as tall as wide.
func CellsFor(m image.Image, maxCols, maxRows int) (int, int) {
	b := m.Bounds()
	w, h := Fit(b.Dx(), b.Dy(), maxCols, maxRows*2)
	return max(w, 1), max((h+1)/2, 1)
}

// Kitty returns the escape sequence that draws m at the cursor in cols×rows
// cells without moving the cursor (C=1).
func Kitty(m image.Image, cols, rows int) string {
	// downscale before sending: ~16 px per cell is plenty for a thumbnail
	b := m.Bounds()
	pw, ph := Fit(b.Dx(), b.Dy(), cols*16, rows*32)
	if pw < b.Dx() {
		m = Resize(m, pw, ph)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		return ""
	}
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	var sb strings.Builder
	first := true
	for len(data) > 0 {
		chunk := data[:min(4096, len(data))]
		data = data[len(chunk):]
		more := 0
		if len(data) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(&sb, "\x1b_Ga=T,f=100,q=2,C=1,c=%d,r=%d,m=%d;%s\x1b\\", cols, rows, more, chunk)
			first = false
		} else {
			fmt.Fprintf(&sb, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
	return sb.String()
}

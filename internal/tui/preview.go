package tui

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/img"
	"github.com/Vaspyyy/glint/internal/peek"
	"github.com/Vaspyyy/glint/internal/project"
	"github.com/Vaspyyy/glint/internal/render"
	"github.com/Vaspyyy/glint/internal/textw"
	"github.com/Vaspyyy/glint/internal/theme"
)

// preview builds the right-hand pane for e, w×h cells.
func (m *model) preview(e *fsx.Entry, w, h int) []string {
	p, r := m.p, m.r
	faint := p.Fg(theme.Faint)
	lines := []string{r.Name(e, true)}

	meta := []string{}
	if !e.IsDir {
		meta = append(meta, humanBytes(e.Size))
	}
	meta = append(meta, e.Mode.String(), relTime(m.now, e.ModTime))
	pk := peek.Of(e)
	line2 := faint.Render(strings.Join(meta, " · "))
	if pk != nil {
		line2 += faint.Render(" · ") + r.PeekText(pk)
	}
	lines = append(lines, line2, faint.Render(strings.Repeat("─", w)))
	body := h - len(lines)

	add := func(ls []string) []string {
		for _, l := range ls {
			if len(lines) >= h {
				break
			}
			lines = append(lines, textw.Truncate(l, w))
		}
		return lines
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Name), "."))
	switch {
	case e.Broken:
		return add([]string{p.Fg(theme.Red).Render("broken link → " + e.Target)})
	case e.IsDir:
		return add(m.dirPreview(e, w, body))
	case !e.Mode.IsRegular() && !e.IsLink, e.IsLink && !fsx.Regular(e.Path):
		return lines // a pipe or a device, also behind a link, whatever it's called: nothing to read
	case img.Supported(ext):
		im, err := img.Thumb(e.Path, max(w, h*2))
		if err != nil {
			return add([]string{p.Fg(theme.Red).Render(err.Error())})
		}
		return add(img.HalfBlocks(im, w, body))
	case ext == "zip" || ext == "jar" || ext == "apk" || ext == "whl" || ext == "epub":
		return add(zipList(e.Path, body))
	case ext == "tar" || ext == "tgz" || strings.HasSuffix(strings.ToLower(e.Name), ".tar.gz"):
		return add(tarList(e.Path, ext != "tar", body))
	}
	return add(m.textPreview(e, ext, w, body))
}

func (m *model) dirPreview(e *fsx.Entry, w, h int) []string {
	var out []string
	if info := project.Detect(e.Path, nil); info != nil {
		out = append(out, m.r.PeekText(&fsx.Peek{Glyph: info.Glyph, Text: info.Label(), Color: info.Color}), "")
	}
	kids, err := fsx.ReadDir(e.Path, fsx.Options{Hidden: btoi(m.hidden)})
	if err != nil {
		return append(out, m.p.Fg(theme.Red).Render("⚠ "+render.ErrMessage(err)))
	}
	if len(kids) == 0 {
		return append(out, m.p.Fg(theme.Faint).Italic().Render("∅ empty"))
	}
	fsx.Sort(kids, m.sortKey, m.reverse, "first")
	room := h - len(out)
	for i, k := range kids {
		if i == room-1 && len(kids) > room {
			out = append(out, m.p.Fg(theme.Faint).Render(fmt.Sprintf("… %d more", len(kids)-i)))
			break
		}
		out = append(out, m.r.Name(k, false))
	}
	return out
}

func (m *model) textPreview(e *fsx.Entry, ext string, w, h int) []string {
	f, err := fsx.Open(e.Path)
	if err != nil {
		return []string{m.p.Fg(theme.Red).Render("⚠ " + render.ErrMessage(err))}
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, 256<<10))
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return hexdump(m.p, data, h)
	}
	raw := strings.Split(strings.ReplaceAll(string(data), "\t", "    "), "\n")
	if len(raw) > h {
		raw = raw[:h]
	}
	for i := range raw {
		raw[i] = strings.TrimRight(raw[i], "\r")
	}
	var styled []string
	key := ext
	if key == "" {
		key = strings.ToLower(e.Name)
	}
	if ext == "md" || ext == "markdown" {
		styled = markdown(m.p, raw)
	} else if l, ok := langByExt[key]; ok || bytes.HasPrefix(data, []byte("#!")) {
		if !ok {
			l = hash
		}
		hl := newHL(m.p, l)
		for _, line := range raw {
			styled = append(styled, hl.line(line))
		}
	} else {
		for _, line := range raw {
			styled = append(styled, m.p.Fg(theme.Text).Render(line))
		}
	}
	gutter := len(fmt.Sprint(len(raw)))
	num := m.p.Fg(theme.Faint)
	for i := range styled {
		styled[i] = num.Render(fmt.Sprintf("%*d ", gutter, i+1)) + styled[i]
	}
	return styled
}

func hexdump(p *theme.Painter, data []byte, h int) []string {
	off, hx, asc := p.Fg(theme.Faint), p.Fg(theme.Blue), p.Fg(theme.Subtle)
	var out []string
	for i := 0; i < len(data) && len(out) < h; i += 16 {
		chunk := data[i:min(i+16, len(data))]
		var hs, as strings.Builder
		for j, c := range chunk {
			if j == 8 {
				hs.WriteByte(' ')
			}
			fmt.Fprintf(&hs, "%02x ", c)
			if c >= 32 && c < 127 {
				as.WriteByte(c)
			} else {
				as.WriteByte('.')
			}
		}
		out = append(out, off.Render(fmt.Sprintf("%06x  ", i))+hx.Render(fmt.Sprintf("%-49s", hs.String()))+asc.Render(as.String()))
	}
	return out
}

func zipList(path string, h int) []string {
	r, err := zip.OpenReader(path)
	if err != nil {
		return []string{err.Error()}
	}
	defer r.Close()
	var out []string
	for i, f := range r.File {
		if i >= h {
			out = append(out, fmt.Sprintf("… %d more", len(r.File)-i))
			break
		}
		out = append(out, fmt.Sprintf("%8s  %s", humanBytes(int64(f.UncompressedSize64)), f.Name))
	}
	return out
}

func tarList(path string, gz bool, h int) []string {
	f, err := fsx.Open(path)
	if err != nil {
		return []string{err.Error()}
	}
	defer f.Close()
	var rd io.Reader = f
	if gz {
		zr, err := gzip.NewReader(f)
		if err != nil {
			return []string{err.Error()}
		}
		rd = zr
	}
	tr := tar.NewReader(rd)
	var out []string
	for len(out) < h {
		hd, err := tr.Next()
		if err != nil {
			break
		}
		out = append(out, fmt.Sprintf("%8s  %s", humanBytes(hd.Size), hd.Name))
	}
	return out
}

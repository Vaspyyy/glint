package render

import (
	"fmt"
	"image"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/img"
	"github.com/Vaspyyy/glint/internal/project"
	"github.com/Vaspyyy/glint/internal/textw"
	"github.com/Vaspyyy/glint/internal/theme"
)

func entrySize(e *fsx.Entry) int64 {
	if e.HasDisk {
		return e.DiskSize
	}
	if e.HasDirSize {
		return e.DirSize
	}
	if e.IsDir {
		return 0
	}
	return e.Size
}

// measure records per-listing maxima used by the bar and heat columns.
func (r *Renderer) measure(rows []Row) {
	r.maxSize, r.maxHeat = 0, 0
	for _, row := range rows {
		if row.E == nil {
			continue
		}
		r.maxSize = max(r.maxSize, entrySize(row.E))
		if row.E.Commit != nil {
			r.maxHeat = max(r.maxHeat, row.E.Commit.Count)
		}
	}
}

// names renders prefix+name for every row and, when peeking, the column width
// names get padded to so the peek text lines up.
func (r *Renderer) names(rows []Row) ([]string, int) {
	out := make([]string, len(rows))
	maxW := 0
	for i, row := range rows {
		if row.E == nil {
			continue
		}
		out[i] = r.faint.Render(row.Prefix) + r.Name(row.E, true)
		if r.cfg.Peek {
			maxW = max(maxW, textw.Width(out[i]))
		}
	}
	return out, min(maxW, max(24, r.cfg.Width/2))
}

func (r *Renderer) withPeek(name string, w int, e *fsx.Entry) string {
	if !r.cfg.Peek || e.Peek == nil {
		return name
	}
	pad := ""
	if r.cfg.Icons && e.Peek.Glyph == "" {
		pad = "  " // keep text aligned with glyph-bearing peeks
	}
	return textw.Pad(name, w) + "  " + pad + r.PeekText(e.Peek)
}

func (r *Renderer) PeekText(p *fsx.Peek) string {
	if p == nil {
		return ""
	}
	st := r.fg[p.Color]
	if p.Color == theme.Muted {
		st = st.Italic()
	}
	s := ""
	if p.Glyph != "" && r.cfg.Icons {
		s = r.fg[p.Color].Render(p.Glyph) + " "
	}
	return s + st.Render(p.Text)
}

var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// Bar draws v/maxV as a smooth block bar of width cells.
func Bar(v, maxV float64, width int) string {
	if maxV <= 0 || v <= 0 {
		return strings.Repeat(" ", width)
	}
	units := int(math.Round(v / maxV * float64(width*8)))
	units = max(units, 1)
	full, rem := units/8, units%8
	s := strings.Repeat("█", full) + eighths[rem]
	return s + strings.Repeat(" ", max(0, width-full-btoi(rem > 0)))
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (r *Renderer) bar(e *fsx.Entry) string {
	const w = 12
	v := float64(entrySize(e))
	frac := 0.0
	if r.maxSize > 0 {
		frac = v / float64(r.maxSize)
	}
	color := r.p.Blend(theme.Green, theme.Yellow, min(frac*2, 1))
	if frac > 0.5 {
		color = r.p.Blend(theme.Yellow, theme.Red, (frac-0.5)*2)
	}
	return r.p.Hex(color).Render(Bar(v, float64(r.maxSize), w)) + r.faint.Render("▏")
}

var sparks = []rune("▁▂▃▄▅▆▇█")

func (r *Renderer) heat(e *fsx.Entry) string {
	if e.Commit == nil || r.maxHeat == 0 {
		return r.faint.Render("·")
	}
	// log scale: one monster folder shouldn't flatten everything else to ▁
	frac := math.Log1p(float64(e.Commit.Count)) / math.Log1p(float64(r.maxHeat))
	idx := min(int(frac*float64(len(sparks)-1)+0.5), len(sparks)-1)
	color := r.p.Blend(theme.Teal, theme.Yellow, min(frac*2, 1))
	if frac > 0.5 {
		color = r.p.Blend(theme.Yellow, theme.Red, (frac-0.5)*2)
	}
	return r.p.Hex(color).Render(string(sparks[idx])+" "+strconv.Itoa(e.Commit.Count)) + ""
}

func (r *Renderer) commit(e *fsx.Entry) string {
	c := e.Commit
	if c == nil {
		return r.faint.Render("·")
	}
	subjectW := 32
	if r.cfg.Width < 140 {
		subjectW = 22
	}
	author, _, _ := strings.Cut(c.Author, "@") // emails as author names are common
	return r.fg[theme.Yellow].Render(c.Hash) + " " + r.fg[theme.Text].Render(textw.Pad(c.Subject, subjectW)) + " " +
		r.ageStyle(r.cfg.Now.Sub(c.Time)).Render(shortAge(r.cfg.Now.Sub(c.Time).Hours())) +
		r.faint.Render(" · "+textw.Pad(author, 12))
}

func shortAge(hours float64) string {
	switch {
	case hours < 1:
		return fmt.Sprintf("%2dm", int(hours*60))
	case hours < 24:
		return fmt.Sprintf("%2dh", int(hours))
	case hours < 24*30:
		return fmt.Sprintf("%2dd", int(hours/24))
	case hours < 24*365:
		return fmt.Sprintf("%2dM", int(hours/24/30))
	}
	return fmt.Sprintf("%2dy", int(hours/24/365))
}

// ---------------------------------------------------------------- card

func (r *Renderer) Card(st *project.Stats, name string) string {
	width := min(max(r.cfg.Width, 40), 84)
	inner := width - 4
	border := r.faint
	var lines []string

	if in := st.Info; in != nil {
		head := r.fg[in.Color].Bold().Render(in.Stack)
		if r.cfg.Icons {
			head = r.fg[in.Color].Render(in.Glyph) + " " + head
		}
		if in.Name != "" {
			head += r.faint.Render(" · ") + r.fg[theme.Text].Render(in.Name)
		}
		if in.Detail != "" {
			head += r.faint.Render(" · ") + r.fg[theme.Subtle].Render(in.Detail)
		}
		lines = append(lines, head)
	}
	if repo := st.Repo; repo != nil {
		br := repo.Branch
		if r.cfg.Icons {
			br = "\ue725 " + br
		}
		s := r.fg[theme.Purple].Bold().Render(br)
		if repo.Ahead > 0 {
			s += r.fg[theme.Green].Render(fmt.Sprintf(" ↑%d", repo.Ahead))
		}
		if repo.Behind > 0 {
			s += r.fg[theme.Red].Render(fmt.Sprintf(" ↓%d", repo.Behind))
		}
		if repo.Dirty() {
			sg, m, u := repo.Counts()
			s += r.fg[theme.Yellow].Render(fmt.Sprintf(" ●%d", sg+m+u))
		} else {
			s += r.fg[theme.Green].Render(" ✓")
		}
		if c := st.Last; c != nil {
			s += r.faint.Render("  ") + r.fg[theme.Yellow].Render(c.Hash) + " " + r.fg[theme.Text].Render(c.Subject) +
				r.faint.Render(" · "+relative(r.cfg.Now.Sub(c.Time)))
		}
		lines = append(lines, s)
	}
	if st.Lines > 0 {
		lines = append(lines, "", r.langBar(st.Langs, st.Lines, inner), r.langLegend(st.Langs, st.Lines, inner))
		todo := ""
		if st.Todos > 0 {
			todo = r.faint.Render(" · ") + r.fg[theme.Orange].Render(strconv.Itoa(st.Todos)) + r.faint.Render(" TODO/FIXME")
		}
		capped := ""
		if st.Capped {
			capped = r.faint.Render(" (capped)")
		}
		lines = append(lines, r.fg[theme.Text].Bold().Render(groupDigits(int64(st.Lines)))+r.faint.Render(" lines in ")+
			r.fg[theme.Text].Bold().Render(groupDigits(int64(st.Files)))+r.faint.Render(" files")+todo+capped)
	}
	if len(lines) == 0 {
		lines = append(lines, r.faint.Italic().Render("nothing to report"))
	}

	title := " " + r.p.Gradient(name, theme.Blue, theme.Pink, true) + " "
	if r.cfg.Icons {
		title = " " + r.fg[theme.Blue].Render("\uf07c") + title
	}
	var b strings.Builder
	b.WriteString(border.Render("╭─") + title + border.Render(strings.Repeat("─", max(0, width-3-textw.Width(title)))+"╮") + "\n")
	for _, l := range lines {
		b.WriteString(border.Render("│ ") + textw.Pad(l, inner) + border.Render(" │") + "\n")
	}
	b.WriteString(border.Render("╰" + strings.Repeat("─", width-2) + "╯"))
	return b.String()
}

func (r *Renderer) langBar(langs []project.Lang, total, width int) string {
	var b strings.Builder
	used := 0
	for i, l := range langs {
		cells := int(math.Round(float64(l.Lines) / float64(total) * float64(width)))
		if i == len(langs)-1 || used+cells > width {
			cells = width - used
		}
		if cells <= 0 {
			continue
		}
		b.WriteString(r.fg[l.Color].Render(strings.Repeat("━", cells)))
		used += cells
		if used >= width {
			break
		}
	}
	return b.String()
}

func (r *Renderer) langLegend(langs []project.Lang, total, width int) string {
	var parts []string
	used := 0
	for _, l := range langs {
		pct := float64(l.Lines) / float64(total) * 100
		if pct < 0.5 {
			break
		}
		p := r.fg[l.Color].Render("●") + " " + r.fg[theme.Text].Render(l.Name) + " " + r.faint.Render(fmt.Sprintf("%.1f%%", pct))
		pw := textw.Width(p) + 2
		if used+pw > width {
			break
		}
		parts = append(parts, p)
		used += pw
	}
	return strings.Join(parts, "  ")
}

// ---------------------------------------------------------------- treemap

type rectf struct{ x, y, w, h float64 }

func worst(row []float64, short float64) float64 {
	s, mx, mn := 0.0, 0.0, math.Inf(1)
	for _, v := range row {
		s += v
		mx = max(mx, v)
		mn = min(mn, v)
	}
	return max(short*short*mx/(s*s), s*s/(short*short*mn))
}

// squarify lays out descending areas (summing to r.w*r.h) as near-square rects.
func squarify(vals []float64, r rectf) []rectf {
	out := make([]rectf, 0, len(vals))
	for i := 0; i < len(vals); {
		short := min(r.w, r.h)
		row := []float64{vals[i]}
		j := i + 1
		for j < len(vals) {
			next := append(append([]float64{}, row...), vals[j])
			if worst(next, short) > worst(row, short) {
				break
			}
			row = next
			j++
		}
		s := 0.0
		for _, v := range row {
			s += v
		}
		if r.w >= r.h {
			cw := s / r.h
			y := r.y
			for _, v := range row {
				h := v / cw
				out = append(out, rectf{r.x, y, cw, h})
				y += h
			}
			r.x += cw
			r.w -= cw
		} else {
			rh := s / r.w
			x := r.x
			for _, v := range row {
				w := v / rh
				out = append(out, rectf{x, r.y, w, rh})
				x += w
			}
			r.y += rh
			r.h -= rh
		}
		i = j
	}
	return out
}

// HumanSize is a compact size like "29 GB".
func HumanSize(n int64) string { return humanLabel(n) }

func humanLabel(n int64) string {
	num, u := humanSize(n)
	return num + " " + units[u]
}

// ---------------------------------------------------------------- thumbnails

const (
	tileW    = 18
	tileRows = 8
)

// Thumbs draws a grid of image thumbnails (or big icons for other files).
func (r *Renderer) Thumbs(w io.Writer, es []*fsx.Entry, kitty bool) {
	if len(es) == 0 {
		return
	}
	imgs := make([]image.Image, len(es))
	fsx.ParallelMin(len(es), 2, func(i int) {
		e := es[i]
		if !e.IsDir && !e.Broken && img.Supported(extOf(e.Name)) {
			if m, err := img.Thumb(e.Path, 64); err == nil {
				imgs[i] = m
			}
		}
	})

	const gap = 2
	cols := max(1, (r.cfg.Width+gap)/(tileW+gap))
	f := &flusher{w: w}
	for start := 0; start < len(es); start += cols {
		end := min(start+cols, len(es))
		row := es[start:end]
		tiles := make([][]string, len(row))
		for i, e := range row {
			if m := imgs[start+i]; m != nil && !kitty {
				lines := img.HalfBlocks(m, tileW, tileRows)
				pad := (tileRows - len(lines)) / 2
				t := make([]string, tileRows)
				for l := range t {
					if l-pad >= 0 && l-pad < len(lines) {
						t[l] = center(lines[l-pad], tileW)
					} else {
						t[l] = spaces(tileW)
					}
				}
				tiles[i] = t
			} else if imgs[start+i] == nil {
				tiles[i] = r.iconTile(e)
			} else {
				tiles[i] = make([]string, tileRows)
				for l := range tiles[i] {
					tiles[i][l] = spaces(tileW)
				}
			}
		}
		for l := range tileRows {
			for i := range row {
				f.WriteString(tiles[i][l])
				if i < len(row)-1 {
					f.WriteString(spaces(gap))
				}
			}
			f.line()
		}
		if kitty {
			// jump back up and drop each image into its reserved tile
			f.WriteString(cursor('A', tileRows))
			for i := range row {
				if m := imgs[start+i]; m != nil {
					c, rr := img.CellsFor(m, tileW, tileRows)
					col := i*(tileW+gap) + (tileW-c)/2
					down := (tileRows - rr) / 2
					f.WriteString("\r" + cursor('C', col) + cursor('B', down) + img.Kitty(m, c, rr) + cursor('A', down))
				}
			}
			f.WriteString("\r" + cursor('B', tileRows))
		}
		for i, e := range row {
			ic, kind := Lookup(e)
			label := e.Name
			name := textw.Truncate(label, tileW-2)
			s := r.nameStyle(e, kind).Render(name)
			if r.cfg.Icons {
				s = r.fg[ic.Color].Render(ic.Glyph) + " " + s
			}
			f.WriteString(center(s, tileW))
			if i < len(row)-1 {
				f.WriteString(spaces(gap))
			}
		}
		f.line()
		f.line()
	}
	f.flush()
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[i+1:]
	}
	return ""
}

func center(s string, w int) string {
	sw := textw.Width(s)
	if sw >= w {
		return textw.Truncate(s, w)
	}
	left := (w - sw) / 2
	return spaces(left) + s + spaces(w-sw-left)
}

func (r *Renderer) iconTile(e *fsx.Entry) []string {
	ic, _ := Lookup(e)
	t := make([]string, tileRows)
	for l := range t {
		t[l] = spaces(tileW)
	}
	box := r.p.On(r.fg[ic.Color].Bold(), theme.Surface)
	inner := tileW - 6
	for l := 1; l < tileRows-1; l++ {
		content := spaces(inner)
		if l == tileRows/2-1 {
			content = center(ic.Glyph, inner)
		}
		if l == tileRows/2 && e.Peek != nil {
			content = center(textw.Truncate(e.Peek.Text, inner-1), inner)
		}
		t[l] = spaces(3) + box.Render(content) + spaces(3)
	}
	return t
}

func cursor(dir byte, n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("\x1b[%d%c", n, dir)
}

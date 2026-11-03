package render

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/textw"
	"github.com/Vaspyyy/glint/internal/theme"
)

// MapItem is one block of a treemap. E is nil for the "+N more" block.
type MapItem struct {
	Label string
	Size  int64
	Regen int64
	E     *fsx.Entry
	// cell rectangle, end-exclusive, without the gutter
	X0, Y0, X1, Y1 int
}

func (it *MapItem) Regenerable() bool { return it.Size > 0 && it.Regen*2 >= it.Size }

// Map is a laid-out treemap.
type Map struct {
	Items         []*MapItem
	Width, Height int
	Total, Regen  int64
	owner         [][]int
}

// Layout computes a squarified treemap of es inside width×height cells.
func Layout(es []*fsx.Entry, width, height int) *Map {
	m := &Map{Width: width, Height: height}
	var items []*MapItem
	for _, e := range es {
		if s := entrySize(e); s > 0 && e.Name != "." && e.Name != ".." {
			items = append(items, &MapItem{Label: e.Name, Size: s, Regen: e.Regen, E: e})
			m.Total += s
			m.Regen += e.Regen
		}
	}
	if m.Total == 0 || width < 4 || height < 2 {
		return m
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Size > items[j].Size })
	// fold the long tail into one block so every rectangle stays legible
	minShare := 1.5 / float64(width*height) * 8
	for i := range items {
		if float64(items[i].Size)/float64(m.Total) < minShare && len(items)-i > 1 {
			rest := &MapItem{Label: fmt.Sprintf("+%d more", len(items)-i)}
			for _, it := range items[i:] {
				rest.Size += it.Size
				rest.Regen += it.Regen
			}
			items = append(items[:i], rest)
			break
		}
	}
	area := float64(width * height * 2)
	vals := make([]float64, len(items))
	for i, it := range items {
		vals[i] = float64(it.Size) / float64(m.Total) * area
	}
	rects := squarify(vals, rectf{0, 0, float64(width), float64(height * 2)})
	m.owner = make([][]int, height)
	for y := range m.owner {
		m.owner[y] = make([]int, width)
		for x := range m.owner[y] {
			m.owner[y][x] = -1
		}
	}
	for i, rc := range rects {
		it := items[i]
		x0, x1 := int(math.Round(rc.x)), min(int(math.Round(rc.x+rc.w)), width)
		y0, y1 := int(math.Round(rc.y/2)), min(int(math.Round((rc.y+rc.h)/2)), height)
		// one-cell gutter right/bottom so neighbours stay distinct
		if x1-x0 > 1 && x1 < width {
			x1--
		}
		if y1-y0 > 1 && y1 < height {
			y1--
		}
		it.X0, it.Y0, it.X1, it.Y1 = x0, y0, x1, y1
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				m.owner[y][x] = i
			}
		}
	}
	m.Items = items
	return m
}

// At returns the index of the block at a cell, or -1.
func (m *Map) At(x, y int) int {
	if y < 0 || y >= len(m.owner) || x < 0 || x >= m.Width {
		return -1
	}
	return m.owner[y][x]
}

// Neighbour finds the block next to sel in direction (dx, dy).
func (m *Map) Neighbour(sel, dx, dy int) int {
	if sel < 0 || sel >= len(m.Items) {
		return 0
	}
	it := m.Items[sel]
	cx, cy := (it.X0+it.X1)/2, (it.Y0+it.Y1)/2
	best, bestD := sel, math.MaxFloat64
	for i, o := range m.Items {
		if i == sel {
			continue
		}
		ox, oy := float64(o.X0+o.X1)/2, float64(o.Y0+o.Y1)/2
		ddx, ddy := ox-float64(cx), (oy-float64(cy))*2 // cells are ~2x taller than wide
		if (dx != 0 && ddx*float64(dx) <= 0) || (dy != 0 && ddy*float64(dy) <= 0) {
			continue
		}
		// prefer blocks straight ahead over ones off to the side
		var d float64
		if dx != 0 {
			d = math.Abs(ddx) + 2*math.Abs(ddy)
		} else {
			d = math.Abs(ddy) + 2*math.Abs(ddx)
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func (r *Renderer) mapColor(i int, it *MapItem) string {
	if it.E == nil {
		return r.p.Pal[theme.Faint]
	}
	return r.p.Pal[theme.Accents[i%len(theme.Accents)]]
}

// DrawMap renders the map; sel (or -1) is drawn highlighted.
func (r *Renderer) DrawMap(m *Map, sel int) []string {
	lines := make([]string, m.Height)
	if len(m.Items) == 0 {
		return lines
	}
	chars := make([][]rune, m.Height)
	for y := range chars {
		chars[y] = []rune(strings.Repeat(" ", m.Width))
	}
	for _, it := range m.Items {
		hatch := it.Regenerable()
		for y := it.Y0; y < it.Y1; y++ {
			for x := it.X0; x < it.X1; x++ {
				if hatch && (x+y)%2 == 0 {
					chars[y][x] = '╱'
				}
			}
		}
		innerW := it.X1 - it.X0 - 1
		if innerW < 3 {
			continue
		}
		labels := []string{it.Label, humanLabel(it.Size)}
		if it.E != nil && r.cfg.Icons {
			ic, _ := Lookup(it.E)
			labels[0] = ic.Glyph + " " + it.Label
		}
		if hatch {
			labels = append(labels, r.recycle()+" rebuildable")
		} else if it.Regen > 0 && it.Regen*10 >= it.Size {
			labels = append(labels, r.recycle()+" "+humanLabel(it.Regen))
		}
		for li, l := range labels {
			y := it.Y0 + li
			if y >= it.Y1 {
				break
			}
			x := it.X0 + 1
			for _, ch := range []rune(textw.Truncate(l, innerW)) {
				if x >= it.X1 {
					break
				}
				chars[y][x] = ch
				x++
			}
			// keep a space after the label so the hatch doesn't touch it
			if x < it.X1 {
				chars[y][x] = ' '
			}
		}
	}
	base := r.p.Pal[theme.Base]
	for y := range m.Height {
		var b strings.Builder
		for x := 0; x < m.Width; {
			o := m.owner[y][x]
			x1 := x
			for x1 < m.Width && m.owner[y][x1] == o {
				x1++
			}
			run := string(chars[y][x:x1])
			if o < 0 {
				b.WriteString(run)
				x = x1
				continue
			}
			it := m.Items[o]
			bg := r.mapColor(o, it)
			fgHex := base
			if it.Regenerable() {
				// hatched: dimmed fill, so "free space" reads at a glance
				fgHex = r.p.Pal[theme.Text]
				bg = blendHex(r, bg, base, 0.55)
			}
			if o == sel {
				bg = blendHex(r, bg, r.p.Pal[theme.Text], 0.45)
				fgHex = base
			}
			st := r.p.Hex(fgHex)
			if y == it.Y0 || o == sel {
				st = st.Bold()
			}
			b.WriteString(r.p.OnHex(st, bg).Render(run))
			x = x1
		}
		lines[y] = b.String()
	}
	return lines
}

func blendHex(_ *Renderer, a, b string, t float64) string { return theme.BlendHex(a, b, t) }

// Treemap prints a map of es plus a callout about reclaimable space.
func (r *Renderer) Treemap(w io.Writer, es []*fsx.Entry, regens []fsx.Regen, width, height int) {
	m := Layout(es, width, height)
	if len(m.Items) == 0 {
		fmt.Fprintln(w, r.Empty())
		return
	}
	f := &flusher{w: w}
	for _, l := range r.DrawMap(m, -1) {
		f.WriteString(l)
		f.line()
	}
	f.WriteString(r.Reclaim(m.Total, m.Regen, regens, width))
	f.flush()
}

// Reclaim is the "you could free X" callout under a disk map.
func (r *Renderer) Reclaim(total, regen int64, regens []fsx.Regen, width int) string {
	if regen < 64<<20 {
		return ""
	}
	var b strings.Builder
	pct := float64(regen) / float64(total) * 100
	b.WriteString("\n" + r.fg[theme.Green].Bold().Render(r.recycle()+" "+humanLabel(regen)) +
		r.fg[theme.Text].Render(fmt.Sprintf(" of %s (%.0f%%) is build output and caches you can regenerate", humanLabel(total), pct)) + "\n")
	for i, g := range regens {
		if i == 6 || g.Size < 16<<20 {
			break
		}
		size := r.sizeText(g.Size)
		line := "  " + spaces(9-textw.Width(size)) + size + "  " + r.fg[theme.Blue].Render(g.Path+"/") + r.faint.Render("  "+g.Hint)
		b.WriteString(textw.Truncate(line, width) + "\n")
	}
	return b.String()
}

// recycle marks regenerable space (Nerd Font icon, or a plain fallback).
func (r *Renderer) recycle() string {
	if r.cfg.Icons {
		return "\uf1b8"
	}
	return "~"
}

// Recycle is the regenerable-space marker for other packages.
func (r *Renderer) Recycle() string { return r.recycle() }

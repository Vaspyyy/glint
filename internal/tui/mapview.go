package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/render"
	"github.com/Vaspyyy/glint/internal/textw"
	"github.com/Vaspyyy/glint/internal/theme"
)

type mapState struct {
	dir      string
	es       []*fsx.Entry
	regens   []fsx.Regen
	lay      *render.Map
	sel      int
	selName  string
	loading  bool
	started  time.Time
	lw, lh   int
	gen      int
	lastPick time.Time
}

const mapTop = 3 // header rows above the map

func (m *model) openMap(dir string) {
	if m.mp == nil {
		m.mp = &mapState{}
	}
	mp := m.mp
	m.mode = modeMap
	focus := ""
	if mp.dir != "" && filepath.Dir(mp.dir) == dir {
		focus = filepath.Base(mp.dir) // coming back up: keep the folder we were in selected
	}
	mp.dir, mp.es, mp.lay, mp.sel, mp.selName = dir, nil, nil, 0, focus
	mp.loading, mp.started = true, time.Now()
	mp.gen++
	gen := mp.gen
	hidden := btoi(m.hidden)
	go func() {
		es, err := fsx.ReadDir(dir, fsx.Options{Hidden: max(hidden, 1)})
		var regens []fsx.Regen
		if err == nil {
			regens = fsx.DiskUsage(es)
		}
		m.events <- func() {
			if gen != mp.gen {
				return
			}
			mp.loading = false
			if err != nil {
				m.msg = "⚠ " + render.ErrMessage(err)
				return
			}
			mp.es, mp.regens = es, regens
			mp.lay = nil
		}
	}()
}

func (m *model) layoutMap() {
	mp := m.mp
	w, h := m.w, max(4, m.h-mapTop-1)
	if mp.lay != nil && mp.lw == w && mp.lh == h {
		return
	}
	mp.lay = render.Layout(mp.es, w, h)
	mp.lw, mp.lh = w, h
	mp.sel = 0
	for i, it := range mp.lay.Items {
		if it.E != nil && it.E.Name == mp.selName {
			mp.sel = i
		}
	}
}

func (m *model) mapSelected() *render.MapItem {
	mp := m.mp
	if mp.lay == nil || mp.sel < 0 || mp.sel >= len(mp.lay.Items) {
		return nil
	}
	return mp.lay.Items[mp.sel]
}

func (m *model) selectMap(i int) {
	mp := m.mp
	mp.sel = i
	if it := m.mapSelected(); it != nil && it.E != nil {
		mp.selName = it.E.Name
	}
}

func (m *model) leaveMap(dir, focus string) {
	m.mode = modeBrowse
	if dir != m.dir || focus != "" {
		m.load(dir, focus)
	}
}

func (m *model) drill() {
	it := m.mapSelected()
	if it == nil || it.E == nil {
		return
	}
	if it.E.IsDir && !it.E.IsLink {
		m.openMap(it.E.Abs)
	} else {
		m.msg = it.E.Name + " is a file: o shows it in the browser"
	}
}

func (m *model) handleMap(k key) action {
	mp := m.mp
	if k.mouse {
		if k.button == 0 && k.press && mp.lay != nil {
			i := mp.lay.At(k.x-1, k.y-1-mapTop)
			if i >= 0 {
				double := i == mp.sel && time.Since(mp.lastPick) < 400*time.Millisecond
				m.selectMap(i)
				mp.lastPick = time.Now()
				if double {
					m.drill()
				}
			}
		}
		return actNone
	}
	move := func(dx, dy int) {
		if mp.lay != nil && len(mp.lay.Items) > 0 {
			m.selectMap(mp.lay.Neighbour(mp.sel, dx, dy))
		}
	}
	switch k.name {
	case "left", "h":
		move(-1, 0)
	case "right", "l":
		move(1, 0)
	case "up", "k":
		move(0, -1)
	case "down", "j":
		move(0, 1)
	case "enter":
		m.drill()
	case "backspace", "u", "-":
		if parent := filepath.Dir(mp.dir); parent != mp.dir {
			m.openMap(parent)
		}
	case "o":
		focus := ""
		if it := m.mapSelected(); it != nil && it.E != nil {
			focus = it.E.Name
		}
		m.leaveMap(mp.dir, focus)
	case "q", "esc":
		if m.onlyMap {
			return actQuit
		}
		m.leaveMap(mp.dir, "")
	case "Q", "ctrl+c":
		return actQuitNoCD
	case "?":
		m.help = true
	}
	return actNone
}

func (m *model) mapFrame() string {
	mp := m.mp
	faint := m.p.Fg(theme.Faint)
	var b strings.Builder
	b.WriteString("\x1b[?2026h\x1b[H")
	title := " " + m.r.Title(&fsx.Entry{Abs: mp.dir}, nil)
	if mp.loading {
		spin := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}[int(time.Since(mp.started)/(100*time.Millisecond))%10]
		b.WriteString(textw.Pad(title+faint.Render("  "+spin+" measuring disk usage…"), m.w) + "\x1b[K\r\n\x1b[J")
		b.WriteString("\x1b[?2026l")
		// keep the spinner moving while we wait
		go func(gen int) {
			time.Sleep(100 * time.Millisecond)
			m.events <- func() {}
		}(mp.gen)
		return b.String()
	}
	m.layoutMap()
	lay := mp.lay
	total := m.p.Fg(theme.Text).Bold().Render(render.HumanSize(lay.Total)) + faint.Render(" on disk")
	if lay.Regen >= 64<<20 {
		total += faint.Render(" · ") + m.p.Fg(theme.Green).Bold().Render(m.r.Recycle()+" "+render.HumanSize(lay.Regen)) +
			faint.Render(fmt.Sprintf(" (%.0f%%) regenerable", float64(lay.Regen)/float64(max(lay.Total, 1))*100))
	}
	b.WriteString(textw.Pad(title+faint.Render("  ·  ")+total, m.w) + "\x1b[K\r\n")

	// selection details
	info := ""
	if it := m.mapSelected(); it != nil {
		name := it.Label
		if it.E != nil && it.E.IsDir {
			name += "/"
		}
		pct := float64(it.Size) / float64(max(lay.Total, 1)) * 100
		info = " " + m.p.Fg(theme.Purple).Bold().Render("▌"+name) + "  " + m.p.Fg(theme.Text).Render(render.HumanSize(it.Size)) +
			faint.Render(fmt.Sprintf("  %.1f%% of this folder", pct))
		if it.Regen > 0 {
			info += faint.Render("  ·  ") + m.p.Fg(theme.Green).Render(m.r.Recycle()+" "+render.HumanSize(it.Regen)+" regenerable")
			if hint := m.regenHint(it); hint != "" {
				info += faint.Render(": " + hint)
			}
		}
	}
	b.WriteString(textw.Pad(info, m.w) + "\x1b[K\r\n\x1b[K\r\n")
	for _, line := range m.r.DrawMap(lay, mp.sel) {
		b.WriteString(line + "\x1b[0m\x1b[K\r\n")
	}
	keys := m.p.Fg(theme.Blue)
	var parts []string
	for _, h := range [][2]string{{"←↑↓→", "select"}, {"enter/click", "open"}, {"⌫", "up"}, {"o", "show in browser"}, {"q", "back"}} {
		parts = append(parts, keys.Render(h[0])+" "+faint.Render(h[1]))
	}
	foot := " " + strings.Join(parts, faint.Render("  "))
	if m.msg != "" {
		foot = " " + m.p.Fg(theme.Green).Render(m.msg)
		m.msg = ""
	}
	b.WriteString(textw.Truncate(foot, m.w) + "\x1b[0m\x1b[K\x1b[J")
	b.WriteString("\x1b[?2026l")
	return b.String()
}

// regenHint explains the biggest regenerable folder inside a block.
func (m *model) regenHint(it *render.MapItem) string {
	if it.E == nil {
		return ""
	}
	for _, g := range m.mp.regens {
		if g.Path == it.E.Name || strings.HasPrefix(g.Path, it.E.Name+"/") {
			return g.Path + "/ → " + g.Hint
		}
	}
	return ""
}

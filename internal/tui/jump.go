package tui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	projects "github.com/Vaspyyy/glint/internal/dash"
	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/glint/internal/textw"
	"github.com/Vaspyyy/glint/internal/theme"
)

// ------------------------------------------------------------------ history

var (
	histMu   sync.Mutex
	history  []string // most recent first
	histOnce sync.Once
)

func historyPath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "glint", "recent")
}

func loadHistory() {
	histOnce.Do(func() {
		f, err := os.Open(historyPath())
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				history = append(history, line)
			}
		}
	})
}

// remember records a visited folder (most recent first, max 100).
func remember(dir string) {
	loadHistory()
	histMu.Lock()
	defer histMu.Unlock()
	out := []string{dir}
	for _, d := range history {
		if d != dir && len(out) < 100 {
			out = append(out, d)
		}
	}
	history = out
	p := historyPath()
	if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
		os.WriteFile(p, []byte(strings.Join(history, "\n")+"\n"), 0o644)
	}
}

// ------------------------------------------------------------------ picker

type pickItem struct {
	name, path string
	glyph      string
	color      theme.Slot
	detail     string // pre-styled
	recent     bool
	active     time.Time
}

type picker struct {
	query   []rune
	items   []pickItem
	view    []int
	cursor  int
	loading bool
}

func (m *model) openPicker() {
	loadHistory()
	pk := &picker{loading: true}
	seen := map[string]bool{}
	histMu.Lock()
	recent := append([]string{}, history...)
	histMu.Unlock()
	n := 0
	for _, d := range recent {
		if d == m.dir || n == 12 {
			continue
		}
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			continue
		}
		seen[d] = true
		pk.items = append(pk.items, pickItem{name: filepath.Base(d), path: d, glyph: "\uf1da", color: theme.Teal,
			detail: m.p.Fg(theme.Faint).Render("recent"), recent: true})
		n++
	}
	m.picker = pk
	m.filterPicker()

	// workspaces = folders holding the repos you've been in
	roots := map[string]bool{}
	for _, d := range append([]string{m.dir}, recent...) {
		if r := git.FindRoot(d); r != "" {
			roots[filepath.Dir(r)] = true
		}
	}
	home, _ := os.UserHomeDir()
	delete(roots, "/")
	go func() {
		var items []pickItem
		for root := range roots {
			if root == home || len(roots) > 8 && !strings.HasPrefix(root, home) {
				continue
			}
			rows, err := projects.Scan(root, false)
			if err != nil {
				continue
			}
			for _, row := range rows {
				if row.Repo == nil && row.Info == nil {
					continue
				}
				it := pickItem{name: row.Name, path: row.Abs, glyph: "\ue5ff", color: theme.Blue, active: row.Active()}
				var parts []string
				if row.Info != nil {
					it.glyph, it.color = row.Info.Glyph, row.Info.Color
					parts = append(parts, m.p.Fg(row.Info.Color).Render(row.Info.Stack))
				}
				switch {
				case row.Repo != nil && row.Repo.NoCommits:
					parts = append(parts, m.p.Fg(theme.Red).Render("no commits"))
				case row.Repo != nil && row.Changes > 0:
					parts = append(parts, m.p.Fg(theme.Yellow).Render(fmt.Sprintf("● %d", row.Changes)))
				case row.Repo != nil:
					parts = append(parts, m.p.Fg(theme.Green).Render("✓"))
				}
				if !it.active.IsZero() {
					parts = append(parts, m.p.Fg(theme.Faint).Render(relTime(time.Now(), it.active)))
				}
				it.detail = strings.Join(parts, m.p.Fg(theme.Faint).Render(" · "))
				items = append(items, it)
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].active.After(items[j].active) })
		m.events <- func() {
			if m.picker != pk {
				return
			}
			for _, it := range items {
				if !seen[it.path] {
					pk.items = append(pk.items, it)
				} else {
					// enrich the recent entry with project facts
					for i := range pk.items {
						if pk.items[i].path == it.path {
							pk.items[i].glyph, pk.items[i].color = it.glyph, it.color
							pk.items[i].detail = m.p.Fg(theme.Faint).Render("recent · ") + it.detail
						}
					}
				}
			}
			pk.loading = false
			m.filterPicker()
		}
	}()
}

func (m *model) filterPicker() {
	pk := m.picker
	pk.view = pk.view[:0]
	q := string(pk.query)
	scores := map[int]int{}
	for i, it := range pk.items {
		if q == "" {
			pk.view = append(pk.view, i)
			continue
		}
		if s, ok := fuzzy(q, it.name); ok {
			scores[i] = s + 30
			pk.view = append(pk.view, i)
		} else if s, ok := fuzzy(q, it.path); ok {
			scores[i] = s
			pk.view = append(pk.view, i)
		}
	}
	if q != "" {
		sort.SliceStable(pk.view, func(a, b int) bool { return scores[pk.view[a]] > scores[pk.view[b]] })
	}
	pk.cursor = min(pk.cursor, max(0, len(pk.view)-1))
}

func (m *model) handlePicker(k key) action {
	pk := m.picker
	switch k.name {
	case "esc":
		m.picker = nil
	case "ctrl+c":
		return actQuitNoCD
	case "up", "shift+tab":
		pk.cursor = max(0, pk.cursor-1)
	case "down", "tab":
		pk.cursor = min(len(pk.view)-1, pk.cursor+1)
	case "enter":
		if pk.cursor < len(pk.view) {
			it := pk.items[pk.view[pk.cursor]]
			m.picker = nil
			if e := m.current(); e != nil {
				m.memory[m.dir] = e.Name
			}
			m.load(it.path, "")
			m.msg = "jumped to " + m.r.PrettyPath(it.path)
		}
	case "backspace":
		if len(pk.query) > 0 {
			pk.query = pk.query[:len(pk.query)-1]
			pk.cursor = 0
			m.filterPicker()
		}
	default:
		if k.text != "" {
			pk.query = append(pk.query, []rune(k.text)...)
			pk.cursor = 0
			m.filterPicker()
		}
	}
	return actNone
}

func (m *model) pickerOverlay() string {
	pk := m.picker
	w := min(max(60, m.w*3/5), m.w-4)
	listH := max(5, min(len(pk.view), m.h-10))
	x := max(1, (m.w-w)/2)
	y := max(2, (m.h-listH-4)/2)
	border := m.p.On(m.p.Fg(theme.Purple), theme.Surface)
	bg := m.p.On(m.p.Plain(), theme.Surface).Seq()
	var b strings.Builder
	put := func(line int, s string) { fmt.Fprintf(&b, "\x1b[%d;%dH%s", y+line, x, s) }
	inner := func(s string) string {
		s = textw.Pad(s, w-2)
		if bg != "" {
			s = bg + strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+bg) + "\x1b[0m"
		}
		return border.Render("│") + s + border.Render("│")
	}
	put(0, border.Render("╭─ ")+m.p.On(m.p.Fg(theme.Purple).Bold(), theme.Surface).Render("Jump to")+border.Render(" "+strings.Repeat("─", max(0, w-12))+"╮"))
	q := m.p.Fg(theme.Text).Render(string(pk.query)) + m.p.Fg(theme.Purple).Render("▏")
	status := ""
	if pk.loading {
		status = m.p.Fg(theme.Faint).Italic().Render("  scanning projects…")
	}
	put(1, inner(" "+m.p.Fg(theme.Blue).Render("›")+" "+q+status))
	start := 0
	if pk.cursor >= listH {
		start = pk.cursor - listH + 1
	}
	for i := 0; i < listH; i++ {
		idx := start + i
		line := ""
		if idx < len(pk.view) {
			it := pk.items[pk.view[idx]]
			bar := "  "
			if idx == pk.cursor {
				bar = m.p.Fg(theme.Purple).Bold().Render(" ▌")
			}
			name := m.p.Fg(theme.Text).Bold().Render(it.name)
			path := m.p.Fg(theme.Faint).Render("  " + m.r.PrettyPath(filepath.Dir(it.path)))
			left := bar + m.p.Fg(it.color).Render(it.glyph) + " " + name + path
			right := it.detail + " "
			line = textw.Pad(left, w-2-textw.Width(right)) + right
		} else if idx == 0 {
			line = m.p.Fg(theme.Faint).Italic().Render("  nothing matches")
		}
		put(2+i, inner(line))
	}
	put(2+listH, border.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return b.String()
}

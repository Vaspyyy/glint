package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/render"
	"github.com/Vaspyyy/glint/internal/textw"
	"github.com/Vaspyyy/glint/internal/theme"
)

const maxHits = 1000

type hit struct {
	path   string // absolute
	line   int
	text   string
	starts []int // byte offsets of matches in text
	ends   []int
}

type searchState struct {
	query   []rune
	hits    []hit
	files   int
	cursor  int
	offset  int
	running bool
	capped  bool
	err     string
	gen     int
	cancel  context.CancelFunc
	timer   *time.Timer
	dir     string
}

func (m *model) openSearch() {
	if _, err := exec.LookPath("rg"); err != nil {
		m.msg = "⚠ content search needs ripgrep (rg)"
		return
	}
	if m.search == nil || m.search.dir != m.dir {
		m.search = &searchState{dir: m.dir}
	}
	m.mode = modeSearch
}

func (m *model) closeSearch() {
	if s := m.search; s != nil {
		if s.cancel != nil {
			s.cancel()
		}
		if s.timer != nil {
			s.timer.Stop()
		}
	}
	m.mode = modeBrowse
}

// schedule debounces typing: a search starts 120ms after the last key.
func (m *model) schedule() {
	s := m.search
	s.gen++
	gen := s.gen
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
	q := string(s.query)
	if strings.TrimSpace(q) == "" {
		s.hits, s.files, s.running, s.err = nil, 0, false, ""
		return
	}
	s.running = true
	s.timer = time.AfterFunc(120*time.Millisecond, func() {
		m.events <- func() {
			if gen == s.gen {
				m.runSearch(q, gen)
			}
		}
	})
}

type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

func (m *model) runSearch(q string, gen int) {
	s := m.search
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	args := []string{"--json", "--smart-case", "--max-columns", "400", "--max-filesize", "4M", "--max-count", "200"}
	if m.hidden {
		args = append(args, "--hidden", "--glob", "!.git")
	}
	args = append(args, "--", q, s.dir)
	cmd := exec.CommandContext(ctx, "rg", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		s.err = err.Error()
		return
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		s.err = err.Error()
		return
	}
	s.hits, s.files, s.cursor, s.offset, s.capped, s.err = nil, 0, 0, 0, false, ""
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 256<<10), 4<<20)
		var batch []hit
		files := 0
		flush := func(done bool) {
			b := batch
			batch = nil
			n := files
			m.events <- func() {
				if gen != s.gen {
					return
				}
				s.hits = append(s.hits, b...)
				s.files = n
				if len(s.hits) >= maxHits {
					s.capped = true
					cancel()
				}
				if done {
					s.running = false
					if msg := strings.TrimSpace(stderr.String()); msg != "" && len(s.hits) == 0 {
						s.err = firstLine(msg)
					}
				}
			}
		}
		last := time.Now()
		for sc.Scan() {
			var ev rgEvent
			if json.Unmarshal(sc.Bytes(), &ev) != nil {
				continue
			}
			switch ev.Type {
			case "begin":
				files++
			case "match":
				h := hit{path: ev.Data.Path.Text, line: ev.Data.LineNumber,
					text: strings.TrimRight(ev.Data.Lines.Text, "\r\n")}
				for _, sm := range ev.Data.Submatches {
					h.starts = append(h.starts, sm.Start)
					h.ends = append(h.ends, sm.End)
				}
				batch = append(batch, h)
			}
			if time.Since(last) > 60*time.Millisecond && len(batch) > 0 {
				flush(false)
				last = time.Now()
			}
		}
		cmd.Wait()
		flush(true)
	}()
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func (m *model) handleSearch(k key) action {
	s := m.search
	if k.mouse {
		switch k.button {
		case 64:
			s.cursor = max(0, s.cursor-3)
		case 65:
			s.cursor = min(len(s.hits)-1, s.cursor+3)
		}
		return actNone
	}
	switch k.name {
	case "esc":
		m.closeSearch()
	case "ctrl+c":
		return actQuitNoCD
	case "up":
		s.cursor = max(0, s.cursor-1)
	case "down", "tab":
		s.cursor = max(0, min(len(s.hits)-1, s.cursor+1))
	case "pgup":
		s.cursor = max(0, s.cursor-(m.h-6))
	case "pgdown":
		s.cursor = max(0, min(len(s.hits)-1, s.cursor+(m.h-6)))
	case "enter":
		if s.cursor < len(s.hits) {
			h := s.hits[s.cursor]
			return m.editAt(h.path, h.line)
		}
	case "ctrl+o": // reveal the file in the browser
		if s.cursor < len(s.hits) {
			h := s.hits[s.cursor]
			m.closeSearch()
			m.load(filepath.Dir(h.path), filepath.Base(h.path))
		}
	case "backspace":
		if len(s.query) > 0 {
			s.query = s.query[:len(s.query)-1]
			m.schedule()
		}
	case "ctrl+u":
		s.query = nil
		m.schedule()
	default:
		if k.text != "" {
			s.query = append(s.query, []rune(k.text)...)
			m.schedule()
		}
	}
	return actNone
}

func (m *model) searchFrame() string {
	s := m.search
	faint := m.p.Fg(theme.Faint)
	var b strings.Builder
	b.WriteString("\x1b[?2026h\x1b[H")

	// header: query input
	icon := "/"
	if m.p.Enabled() {
		icon = "\uf002"
	}
	title := m.p.Fg(theme.Purple).Bold().Render(" "+icon+" Search ") + faint.Render("in "+m.r.PrettyPath(s.dir)+"  ")
	q := m.p.Fg(theme.Text).Bold().Render(string(s.query)) + m.p.Fg(theme.Purple).Render("▏")
	status := ""
	switch {
	case s.err != "":
		status = m.p.Fg(theme.Red).Render(s.err)
	case s.running:
		status = faint.Italic().Render(fmt.Sprintf("searching… %d hits", len(s.hits)))
	case len(s.query) == 0:
		status = faint.Italic().Render("type to search file contents (regex, smart case)")
	default:
		plus := ""
		if s.capped {
			plus = "+"
		}
		status = faint.Render(fmt.Sprintf("%d%s %s in %d %s", len(s.hits), plus, pl(len(s.hits), "hit"), s.files, pl(s.files, "file")))
	}
	b.WriteString(textw.Pad(title+q+"   "+status, m.w) + "\x1b[K\r\n")
	lw := m.listWidth()
	if m.w >= 60 {
		b.WriteString(faint.Render(strings.Repeat("─", lw+1)+"┬"+strings.Repeat("─", max(0, m.w-lw-2))) + "\x1b[K\r\n")
	} else {
		b.WriteString(faint.Render(strings.Repeat("─", m.w)) + "\x1b[K\r\n")
	}

	body := max(1, m.h-3)
	// display rows: a header row for each file, then its hits
	type drow struct {
		hit    int // -1 for a file header
		header string
	}
	var rows []drow
	cursorRow := 0
	for i, h := range s.hits {
		if i == 0 || h.path != s.hits[i-1].path {
			rows = append(rows, drow{-1, h.path})
		}
		if i == s.cursor {
			cursorRow = len(rows)
		}
		rows = append(rows, drow{i, ""})
	}
	if cursorRow-1 < s.offset { // keep the file header of the current hit visible
		s.offset = max(0, cursorRow-1)
	}
	if cursorRow >= s.offset+body {
		s.offset = cursorRow - body + 1
	}
	var right []string
	if s.cursor < len(s.hits) && m.w >= 60 {
		right = m.hitPreview(s.hits[s.cursor], m.w-lw-3, body)
	}
	for row := 0; row < body; row++ {
		idx := s.offset + row
		left := spaces(lw)
		if idx < len(rows) {
			if d := rows[idx]; d.hit < 0 {
				left = textw.Pad(" "+m.fileIcon(d.header)+m.p.Fg(theme.Blue).Bold().Render(m.relPath(d.header)), lw)
			} else {
				left = m.hitRow(s.hits[d.hit], d.hit == s.cursor, lw)
			}
		}
		b.WriteString(left)
		if m.w >= 60 {
			b.WriteString(faint.Render(" │ "))
			if row < len(right) {
				b.WriteString(textw.Truncate(right[row], m.w-lw-3))
			}
		}
		b.WriteString("\x1b[0m\x1b[K\r\n")
	}
	k := m.p.Fg(theme.Blue)
	var parts []string
	for _, h := range [][2]string{{"↑↓", "move"}, {"enter", "edit at line"}, {"ctrl+o", "show in browser"}, {"ctrl+u", "clear"}, {"esc", "back"}} {
		parts = append(parts, k.Render(h[0])+" "+faint.Render(h[1]))
	}
	b.WriteString(textw.Truncate(" "+strings.Join(parts, faint.Render("  ")), m.w) + "\x1b[0m\x1b[K")
	b.WriteString("\x1b[?2026l")
	return b.String()
}

func (m *model) relPath(p string) string {
	if rel, err := filepath.Rel(m.search.dir, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

// hitRow is "file:line  snippet" with the match highlighted. The file name is
// only spelled out on its first hit; later hits indent under it.
func (m *model) hitRow(h hit, cursor bool, w int) string {
	bar := "  "
	if cursor {
		bar = m.p.Fg(theme.Purple).Bold().Render(" ▌")
	}
	loc := m.p.Fg(theme.Faint).Render(fmt.Sprintf("%5d  ", h.line))
	line := bar + loc + m.highlightHit(h, w)
	line = textw.Pad(line, w)
	if cursor {
		if bg := m.p.On(m.p.Plain(), theme.Surface).Seq(); bg != "" {
			line = bg + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bg) + "\x1b[0m"
		}
	}
	return line
}

// highlightHit renders the matched line with matches in bold yellow,
// trimmed so the first match is visible.
func (m *model) highlightHit(h hit, w int) string {
	// expand tabs, moving the match offsets along with the text
	where := make([]int, len(h.text)+1)
	var sb strings.Builder
	for i := 0; i < len(h.text); i++ {
		where[i] = sb.Len()
		if h.text[i] == '\t' {
			sb.WriteString("  ")
		} else {
			sb.WriteByte(h.text[i])
		}
	}
	where[len(h.text)] = sb.Len()
	text := sb.String()
	var starts, ends []int
	for i := range h.starts {
		starts = append(starts, where[min(h.starts[i], len(h.text))])
		ends = append(ends, where[min(h.ends[i], len(h.text))])
	}
	lead := len(text) - len(strings.TrimLeft(text, " "))
	cut := lead
	if len(starts) > 0 && starts[0] > w/2 {
		cut = max(lead, starts[0]-w/4)
	}
	norm := m.p.Fg(theme.Text)
	hl := m.p.Fg(theme.Yellow).Bold().Underline()
	var b strings.Builder
	pos := cut
	if cut > lead {
		b.WriteString(m.p.Fg(theme.Faint).Render("…"))
	}
	for i := range starts {
		st, en := starts[i], ends[i]
		if en <= pos || st >= len(text) {
			continue
		}
		st = max(st, pos)
		b.WriteString(norm.Render(text[pos:st]))
		b.WriteString(hl.Render(text[st:min(en, len(text))]))
		pos = min(en, len(text))
	}
	if pos < len(text) {
		b.WriteString(norm.Render(text[pos:]))
	}
	return b.String()
}

// hitPreview shows the file around the hit, syntax highlighted, hit line marked.
func (m *model) hitPreview(h hit, w, height int) []string {
	data, err := os.ReadFile(h.path)
	if err != nil {
		return []string{m.p.Fg(theme.Red).Render(err.Error())}
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\t", "    "), "\n")
	start := max(0, h.line-1-height/3)
	end := min(len(lines), start+height-2)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(h.path), "."))
	l, ok := langByExt[ext]
	var hl *hl
	if ok {
		hl = newHL(m.p, l)
	}
	out := []string{m.p.Fg(theme.Blue).Bold().Render(m.relPath(h.path)) + m.p.Fg(theme.Faint).Render(fmt.Sprintf(":%d", h.line)), ""}
	gutter := len(fmt.Sprint(end))
	mark := m.p.On(m.p.Plain(), theme.Surface).Seq()
	for i := start; i < end; i++ {
		text := strings.TrimRight(lines[i], "\r")
		styled := m.p.Fg(theme.Text).Render(text)
		if hl != nil {
			styled = hl.line(text)
		}
		num := m.p.Fg(theme.Faint).Render(fmt.Sprintf("%*d ", gutter, i+1))
		row := num + styled
		if i == h.line-1 {
			num = m.p.Fg(theme.Yellow).Bold().Render(fmt.Sprintf("%*d▸", gutter, i+1))
			row = textw.Pad(num+styled, w)
			if mark != "" {
				row = mark + strings.ReplaceAll(row, "\x1b[0m", "\x1b[0m"+mark) + "\x1b[0m"
			}
		}
		out = append(out, row)
	}
	return out
}

func pl(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func (m *model) fileIcon(path string) string {
	e, err := fsx.Stat(path, fsx.Options{})
	if err != nil {
		return ""
	}
	ic, _ := render.Lookup(e)
	return m.p.Fg(ic.Color).Render(ic.Glyph) + " "
}

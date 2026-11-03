// Package tui is glint's interactive browser (glint -i).
//
// It talks to the terminal directly instead of using Bubble Tea: Bubble Tea's
// dependency tree adds ~20ms of init time to every glint invocation, and a
// file browser only needs a handful of escape sequences anyway.
package tui

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/glint/internal/render"
	"github.com/Vaspyyy/glint/internal/textw"
	"github.com/Vaspyyy/glint/internal/theme"
)

type Options struct {
	Painter *theme.Painter
	Render  render.Config
	Start   string
	Hidden  bool
	SortKey string
	Map     bool // start in the disk map
}

const (
	modeBrowse = iota
	modeSearch
	modeMap
)

type previewResult struct {
	key   string
	lines []string
}

type model struct {
	p   *theme.Painter
	r   *render.Renderer
	now time.Time

	dir    string
	repo   *git.Repo
	all    []*fsx.Entry
	view   []int
	err    error
	cursor int
	offset int

	filter    string
	filtering bool
	hidden    bool
	sortKey   string
	reverse   bool
	selected  map[string]bool
	memory    map[string]string // dir → name the cursor was on

	cache    map[string][]string
	pending  map[string]bool
	previews chan previewResult

	w, h      int
	msg       string
	help      bool
	lastClick time.Time

	mode    int
	prompt  *prompt
	picker  *picker
	clip    *clipboard
	search  *searchState
	mp      *mapState
	events  chan func() // work finished in the background, applied on the UI loop
	onlyMap bool        // started with -M -i: leaving the map quits
}

// Run starts the browser and returns the directory it ended in.
func Run(opt Options) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("interactive mode needs a terminal: %w", err)
	}
	defer tty.Close()
	fd := int(tty.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	enter := func() { tty.WriteString("\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h") }
	leave := func() { tty.WriteString("\x1b[?1000l\x1b[?1006l\x1b[?25h\x1b[?1049l") }
	enter()
	defer func() {
		leave()
		term.Restore(fd, state)
	}()

	cfg := opt.Render
	cfg.Icons = cfg.Icons || opt.Painter.Enabled()
	m := &model{
		p: opt.Painter, r: render.New(cfg), now: time.Now(),
		hidden: opt.Hidden, sortKey: opt.SortKey,
		selected: map[string]bool{}, memory: map[string]string{},
		cache: map[string][]string{}, pending: map[string]bool{},
		previews: make(chan previewResult, 8),
		events:   make(chan func(), 16),
	}
	if m.sortKey == "" || m.sortKey == "none" {
		m.sortKey = "name"
	}
	start, _ := filepath.Abs(opt.Start)
	m.load(start, "")
	m.resize(fd)
	if opt.Map {
		m.onlyMap = true
		m.openMap(m.dir)
	}

	in := newInput(tty)
	go in.loop()
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	for {
		tty.WriteString(m.frame())
		select {
		case keys, ok := <-in.keys:
			if !ok {
				return m.dir, nil
			}
			for _, k := range keys {
				switch m.handle(k) {
				case actQuit:
					return m.dir, nil
				case actQuitNoCD:
					return "", nil
				case actEdit:
					m.runExternal(tty, fd, state, in, enter, leave)
				}
			}
		case fn := <-m.events:
			fn()
		case res := <-m.previews:
			m.cache[res.key] = res.lines
			delete(m.pending, res.key)
		case <-winch:
			m.resize(fd)
		}
	}
}

func (m *model) resize(fd int) {
	w, h, err := term.GetSize(fd)
	if err != nil || w <= 0 {
		w, h = 100, 30
	}
	if w != m.w || h != m.h {
		clear(m.cache)
	}
	m.w, m.h = w, h
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (m *model) load(dir, focus string) {
	es, err := fsx.ReadDir(dir, fsx.Options{Hidden: btoi(m.hidden)})
	m.dir, m.err = dir, err
	m.repo, _ = git.Open(dir)
	if m.repo != nil {
		for _, e := range es {
			e.Git = m.repo.Status(e.Abs)
		}
	}
	fsx.Sort(es, m.sortKey, m.reverse, "first")
	m.all = es
	remember(dir)
	m.filter, m.filtering = "", false
	m.applyFilter()
	if focus == "" {
		focus = m.memory[dir]
	}
	m.cursor, m.offset = 0, 0
	for i, idx := range m.view {
		if m.all[idx].Name == focus {
			m.cursor = i
		}
	}
}

func (m *model) current() *fsx.Entry {
	if m.cursor < 0 || m.cursor >= len(m.view) {
		return nil
	}
	return m.all[m.view[m.cursor]]
}

// ------------------------------------------------------------------ filter

// fuzzy scores a case-insensitive subsequence match; ok=false if no match.
func fuzzy(pattern, s string) (int, bool) {
	p := []rune(fsx.Fold(pattern))
	t := []rune(fsx.Fold(s))
	score, pi, prev := 0, 0, -2
	for ti := 0; ti < len(t) && pi < len(p); ti++ {
		if t[ti] != p[pi] {
			continue
		}
		score += 10
		if ti == prev+1 {
			score += 15
		}
		if ti == 0 || strings.ContainsRune("-_. /", t[ti-1]) {
			score += 20
		}
		score -= min(ti-prev-1, 10)
		prev = ti
		pi++
	}
	if pi < len(p) {
		return 0, false
	}
	return score - len(t)/4, true
}

func (m *model) applyFilter() {
	m.view = m.view[:0]
	if m.filter == "" {
		for i := range m.all {
			m.view = append(m.view, i)
		}
		return
	}
	scores := map[int]int{}
	for i, e := range m.all {
		if s, ok := fuzzy(m.filter, e.Name); ok {
			m.view = append(m.view, i)
			scores[i] = s
		}
	}
	sort.SliceStable(m.view, func(a, b int) bool { return scores[m.view[a]] > scores[m.view[b]] })
	m.cursor, m.offset = 0, 0
}

// ------------------------------------------------------------------ actions

type action int

const (
	actNone action = iota
	actQuit
	actQuitNoCD
	actEdit
)

var editTarget struct {
	cmd []string
}

func (m *model) move(d int) {
	m.cursor = max(0, min(len(m.view)-1, m.cursor+d))
	m.msg = ""
}

func (m *model) enter(e *fsx.Entry) action {
	if e == nil {
		return actNone
	}
	if e.IsDir {
		m.memory[m.dir] = e.Name
		m.load(e.Abs, "")
		return actNone
	}
	return m.edit(e)
}

func (m *model) parent() {
	parent := filepath.Dir(m.dir)
	if parent == m.dir {
		return
	}
	if e := m.current(); e != nil {
		m.memory[m.dir] = e.Name
	}
	m.load(parent, filepath.Base(m.dir))
}

func (m *model) edit(e *fsx.Entry) action { return m.editAt(e.Abs, 0) }

func (m *model) open(e *fsx.Entry) {
	if e == nil {
		return
	}
	cmd := exec.Command("xdg-open", e.Abs)
	if err := cmd.Start(); err != nil {
		m.msg = "xdg-open: " + err.Error()
		return
	}
	go cmd.Wait()
	m.msg = "opened " + e.Name
}

func (m *model) copyPaths(tty *os.File) {
	var paths []string
	for p := range m.selected {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		if e := m.current(); e != nil {
			paths = []string{e.Abs}
		}
	}
	if len(paths) == 0 {
		return
	}
	payload := base64.StdEncoding.EncodeToString([]byte(strings.Join(paths, "\n")))
	tty.WriteString("\x1b]52;c;" + payload + "\x07")
	if len(paths) == 1 {
		m.msg = "copied " + paths[0]
	} else {
		m.msg = fmt.Sprintf("copied %d paths", len(paths))
	}
}

// runExternal suspends the UI, runs the editor on the real terminal, resumes.
func (m *model) runExternal(tty *os.File, fd int, state *term.State, in *input, enter, leave func()) {
	in.pause()
	leave()
	term.Restore(fd, state)
	cmd := exec.Command(editTarget.cmd[0], editTarget.cmd[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	err := cmd.Run()
	term.MakeRaw(fd)
	enter()
	in.resume()
	clear(m.cache)
	if err != nil {
		m.msg = err.Error()
	}
	focus := ""
	if e := m.current(); e != nil {
		focus = e.Name
	}
	m.load(m.dir, focus)
}

var sortCycle = []string{"name", "size", "time", "ext"}

func (m *model) handle(k key) action {
	if m.help {
		m.help = false
		return actNone
	}
	switch {
	case m.prompt != nil:
		return m.handlePrompt(k)
	case m.picker != nil:
		return m.handlePicker(k)
	case m.mode == modeSearch:
		return m.handleSearch(k)
	case m.mode == modeMap:
		return m.handleMap(k)
	}
	if k.mouse {
		return m.handleMouse(k)
	}
	if m.filtering {
		switch k.name {
		case "esc":
			m.filter, m.filtering = "", false
			m.applyFilter()
		case "enter":
			m.filtering = false
			if len(m.view) == 1 {
				return m.enter(m.current())
			}
		case "backspace":
			if m.filter == "" {
				m.filtering = false
			} else {
				r := []rune(m.filter)
				m.filter = string(r[:len(r)-1])
				m.applyFilter()
			}
		case "up":
			m.move(-1)
		case "down", "tab":
			m.move(1)
		case "ctrl+c":
			return actQuitNoCD
		default:
			if k.text != "" {
				m.filter += k.text
				m.applyFilter()
			}
		}
		return actNone
	}

	page := max(1, m.h-5)
	switch k.name {
	case "q":
		return actQuit
	case "Q", "ctrl+c":
		return actQuitNoCD
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup", "ctrl+u":
		m.move(-page / 2)
	case "pgdown", "ctrl+d", " ":
		if k.name == " " {
			if e := m.current(); e != nil {
				if m.selected[e.Abs] {
					delete(m.selected, e.Abs)
				} else {
					m.selected[e.Abs] = true
				}
				m.move(1)
			}
		} else {
			m.move(page / 2)
		}
	case "home", "g":
		m.move(-len(m.view))
	case "end", "G":
		m.move(len(m.view))
	case "left", "h", "backspace":
		m.parent()
	case "right", "l":
		if e := m.current(); e != nil && e.IsDir {
			return m.enter(e)
		}
	case "enter":
		return m.enter(m.current())
	case "e":
		if e := m.current(); e != nil && !e.IsDir {
			return m.edit(e)
		}
	case "o":
		m.open(m.current())
	case "/":
		m.filtering = true
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
		} else if len(m.selected) > 0 {
			clear(m.selected)
		}
	case ".":
		m.hidden = !m.hidden
		m.reload()
	case "s":
		for i, s := range sortCycle {
			if s == m.sortKey {
				m.sortKey = sortCycle[(i+1)%len(sortCycle)]
				break
			}
		}
		m.msg = "sort: " + m.sortKey
		m.reload()
	case "R":
		m.reverse = !m.reverse
		m.reload()
	case "r", "f2":
		m.rename()
	case "d", "delete":
		m.trash()
	case "c":
		m.copyCut(false)
	case "x":
		m.copyCut(true)
	case "p":
		m.paste()
	case "N":
		m.newEntry(true)
	case "n":
		m.newEntry(false)
	case "P", "ctrl+p":
		m.openPicker()
	case "f", "ctrl+f":
		m.openSearch()
	case "M":
		m.openMap(m.dir)
	case "~":
		home, _ := os.UserHomeDir()
		m.memory[m.dir] = ""
		m.load(home, "")
	case "y":
		m.copyPaths(ttyOut)
	case "a":
		if len(m.selected) > 0 {
			clear(m.selected)
		} else {
			for _, i := range m.view {
				m.selected[m.all[i].Abs] = true
			}
		}
	case "?":
		m.help = true
	}
	return actNone
}

func (m *model) reload() {
	focus := ""
	if e := m.current(); e != nil {
		focus = e.Name
	}
	clear(m.cache)
	m.load(m.dir, focus)
}

func (m *model) handleMouse(k key) action {
	switch k.button {
	case 64:
		m.move(-3)
	case 65:
		m.move(3)
	case 0:
		if !k.press {
			return actNone
		}
		row := k.y - 3 // 1-based terminal rows; list starts on row 3
		if k.x <= m.listWidth() && row >= 0 && m.offset+row < len(m.view) {
			idx := m.offset + row
			double := idx == m.cursor && time.Since(m.lastClick) < 400*time.Millisecond
			m.cursor = idx
			m.lastClick = time.Now()
			if double {
				return m.enter(m.current())
			}
		}
	}
	return actNone
}

// ------------------------------------------------------------------ drawing

func (m *model) listWidth() int {
	if m.w < 60 {
		return m.w
	}
	return max(30, min(m.w*2/5, 60))
}

func (m *model) frame() string {
	m.now = time.Now()
	switch m.mode {
	case modeSearch:
		return m.searchFrame()
	case modeMap:
		return m.mapFrame()
	}
	var b strings.Builder
	b.WriteString("\x1b[?2026h\x1b[H")
	faint := m.p.Fg(theme.Faint)
	lw := m.listWidth()
	pw := m.w - lw - 3
	body := max(1, m.h-3)

	// header
	title := " " + m.r.Title(&fsx.Entry{Abs: m.dir}, m.repo)
	count := fmt.Sprintf("%d/%d ", min(m.cursor+1, len(m.view)), len(m.view))
	if len(m.selected) > 0 {
		count = m.p.Fg(theme.Pink).Render(fmt.Sprintf("%d selected  ", len(m.selected))) + faint.Render(count)
	} else {
		count = faint.Render(count)
	}
	b.WriteString(textw.Pad(title, m.w-textw.Width(count)) + count + "\x1b[K\r\n")
	if m.w >= 60 {
		b.WriteString(faint.Render(strings.Repeat("─", lw+1)+"┬"+strings.Repeat("─", max(0, m.w-lw-2))) + "\x1b[K\r\n")
	} else {
		b.WriteString(faint.Render(strings.Repeat("─", m.w)) + "\x1b[K\r\n")
	}

	// keep cursor visible
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+body {
		m.offset = m.cursor - body + 1
	}

	var right []string
	if cur := m.current(); cur != nil && pw > 10 {
		key := fmt.Sprintf("%s|%d|%d|%v|%s", cur.Abs, pw, body, m.hidden, cur.ModTime)
		if lines, ok := m.cache[key]; ok {
			right = lines
		} else {
			if !m.pending[key] {
				m.pending[key] = true
				e := cur
				go func() {
					defer func() { recover() }()
					m.previews <- previewResult{key, m.preview(e, pw, body)}
				}()
			}
			right = []string{m.r.Name(cur, true), faint.Italic().Render("loading…")}
		}
	}

	for row := range body {
		idx := m.offset + row
		left := spaces(lw)
		switch {
		case m.err != nil && row == 0:
			left = textw.Pad(m.p.Fg(theme.Red).Render(" ⚠ "+render.ErrMessage(m.err)), lw)
		case len(m.view) == 0 && row == 0:
			msg := " ∅ empty"
			if m.filter != "" {
				msg = " no matches"
			}
			left = textw.Pad(faint.Italic().Render(msg), lw)
		case idx < len(m.view):
			left = m.row(m.all[m.view[idx]], idx == m.cursor, lw)
		}
		b.WriteString(left)
		if m.w >= 60 {
			b.WriteString(faint.Render(" │ "))
			if row < len(right) {
				b.WriteString(textw.Truncate(right[row], pw))
			}
		}
		b.WriteString("\x1b[0m\x1b[K\r\n")
	}

	// footer
	var foot string
	switch {
	case m.prompt != nil:
		foot = m.promptLine()
	case m.filtering:
		foot = m.p.Fg(theme.Purple).Bold().Render(" / ") + m.p.Fg(theme.Text).Render(m.filter) + m.p.Fg(theme.Purple).Render("▏")
	case strings.HasPrefix(m.msg, "⚠"):
		foot = " " + m.p.Fg(theme.Red).Render(m.msg)
	case m.msg != "":
		foot = " " + m.p.Fg(theme.Green).Render(m.msg)
	default:
		foot = m.hints()
	}
	if m.filter != "" && !m.filtering {
		foot = m.p.Fg(theme.Purple).Render(" /"+m.filter) + faint.Render("  esc clears ·") + foot
	}
	b.WriteString(textw.Truncate(foot, m.w) + "\x1b[0m\x1b[K")
	if m.picker != nil {
		b.WriteString(m.pickerOverlay())
	}
	if m.help {
		b.WriteString(m.helpOverlay())
	}
	b.WriteString("\x1b[?2026l")
	return b.String()
}

func spaces(n int) string { return strings.Repeat(" ", max(0, n)) }

func (m *model) hints() string {
	k := m.p.Fg(theme.Blue)
	d := m.p.Fg(theme.Faint)
	var parts []string
	for _, h := range [][2]string{{"↑↓←→", "move"}, {"/", "filter"}, {"f", "search"}, {"P", "projects"}, {"M", "disk map"},
		{"space", "select"}, {"r", "rename"}, {"d", "trash"}, {"c/x/p", "copy/cut/paste"}, {"e", "edit"}, {"q", "quit"}, {"?", "help"}} {
		parts = append(parts, k.Render(h[0])+" "+d.Render(h[1]))
	}
	return " " + strings.Join(parts, d.Render("  "))
}

func (m *model) row(e *fsx.Entry, cursor bool, w int) string {
	mark := " "
	if m.selected[e.Abs] {
		mark = m.p.Fg(theme.Pink).Render("●")
	}
	bar := " "
	if cursor {
		bar = m.p.Fg(theme.Purple).Bold().Render("▌")
	}
	gitMark := " "
	if !e.Git.Clean() {
		gitMark = gitShort(m.p, e.Git)
	}
	left := bar + mark + gitMark + m.r.Name(e, false)
	meta := ""
	if w >= 44 {
		age := m.p.Fg(theme.Faint).Render(relTime(m.now, e.ModTime))
		if e.IsDir {
			meta = age
		} else {
			meta = m.p.Fg(theme.Muted).Render(humanBytes(e.Size)) + "  " + age
		}
	}
	line := textw.Pad(left, w-textw.Width(meta)-1) + meta + " "
	if cursor {
		bg := m.p.On(m.p.Plain(), theme.Surface).Seq()
		if bg != "" {
			line = bg + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bg) + "\x1b[0m"
		}
	}
	return line
}

func gitShort(p *theme.Painter, s git.Status) string {
	c := s.X
	if c == ' ' || c == 0 {
		c = s.Y
	}
	slot := map[byte]theme.Slot{'M': theme.Yellow, 'A': theme.Green, 'D': theme.Red, '?': theme.Teal, '!': theme.Faint, 'R': theme.Purple}[c]
	if slot == 0 {
		slot = theme.Yellow
	}
	return p.Fg(slot).Render(string(c))
}

func (m *model) helpOverlay() string {
	rows := [][2]string{
		{"↑ ↓  j k", "move"}, {"← →  h l", "parent / enter folder"}, {"enter", "open folder · edit file"},
		{"pgup pgdn  ctrl+u/d", "half page"}, {"g G", "top / bottom"}, {"/", "fuzzy filter names"},
		{"f  ctrl+f", "search inside files"}, {"P  ctrl+p", "jump: projects & recent folders"},
		{"M", "disk map of this folder"},
		{"space · a", "select · select all"}, {"r  F2", "rename"}, {"d  del", "move to trash"},
		{"c · x · p", "copy · cut · paste here"}, {"n · N", "new file · new folder"},
		{"y", "copy path(s) to clipboard"}, {"e · o", "$EDITOR · xdg-open"},
		{". · s · R", "hidden · sort · reverse"}, {"~", "home"}, {"q", "quit (cd here with `gi`)"}, {"Q", "quit, stay put"},
	}
	w := 58
	x := max(1, (m.w-w)/2)
	y := max(1, (m.h-len(rows)-4)/2)
	box := m.p.On(m.p.Fg(theme.Text), theme.Surface)
	key := m.p.On(m.p.Fg(theme.Blue).Bold(), theme.Surface)
	border := m.p.On(m.p.Fg(theme.Purple), theme.Surface)
	var b strings.Builder
	put := func(line int, s string) { fmt.Fprintf(&b, "\x1b[%d;%dH%s", y+line, x, s) }
	put(0, border.Render("╭"+strings.Repeat("─", w-2)+"╮"))
	put(1, border.Render("│")+box.Render(textw.Pad("  ✦ glint keys", w-2))+border.Render("│"))
	for i, r := range rows {
		put(i+2, border.Render("│")+key.Render(textw.Pad("  "+r[0], 24))+box.Render(textw.Pad(r[1], w-26))+border.Render("│"))
	}
	put(len(rows)+2, border.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return b.String()
}

func humanBytes(n int64) string {
	const u = "KMGTP"
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	f := float64(n)
	i := -1
	for f >= 1024 && i < len(u)-1 {
		f /= 1024
		i++
	}
	if f < 10 {
		return fmt.Sprintf("%.1f %cB", f, u[i])
	}
	return fmt.Sprintf("%.0f %cB", f, u[i])
}

func relTime(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/24/365))
}

// ------------------------------------------------------------------ input

type key struct {
	name         string // "up", "enter", "ctrl+c", "a", ...
	text         string // printable text for filter typing
	mouse        bool
	button, x, y int
	press        bool
}

var ttyOut *os.File

type input struct {
	tty     *os.File
	keys    chan []key
	paused  chan struct{}
	resumeC chan struct{}
}

func newInput(tty *os.File) *input {
	ttyOut = tty
	return &input{tty: tty, keys: make(chan []key, 16), paused: make(chan struct{}), resumeC: make(chan struct{})}
}

func (in *input) loop() {
	buf := make([]byte, 1024)
	for {
		n, err := in.tty.Read(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				in.paused <- struct{}{}
				<-in.resumeC
				continue
			}
			close(in.keys)
			return
		}
		if ks := parseKeys(buf[:n]); len(ks) > 0 {
			in.keys <- ks
		}
	}
}

// pause interrupts the blocked read so the editor gets the keyboard.
func (in *input) pause() {
	if err := in.tty.SetReadDeadline(time.Now()); err != nil {
		return
	}
	<-in.paused
}

func (in *input) resume() {
	in.tty.SetReadDeadline(time.Time{})
	select {
	case in.resumeC <- struct{}{}:
	case <-time.After(100 * time.Millisecond):
	}
}

func parseKeys(b []byte) []key {
	var out []key
	for len(b) > 0 {
		k, n := parseKey(b)
		if n <= 0 {
			n = 1
		}
		b = b[n:]
		if k.name != "" || k.mouse {
			out = append(out, k)
		}
	}
	return out
}

func parseKey(b []byte) (key, int) {
	c := b[0]
	if c == 0x1b {
		if len(b) == 1 {
			return key{name: "esc"}, 1
		}
		if b[1] == '[' || b[1] == 'O' {
			if len(b) > 2 && b[2] == '<' { // SGR mouse: ESC [ < b ; x ; y (M|m)
				end := 3
				for end < len(b) && b[end] != 'M' && b[end] != 'm' {
					end++
				}
				if end >= len(b) {
					return key{}, len(b)
				}
				f := strings.Split(string(b[3:end]), ";")
				if len(f) == 3 {
					btn, _ := strconv.Atoi(f[0])
					x, _ := strconv.Atoi(f[1])
					y, _ := strconv.Atoi(f[2])
					return key{mouse: true, button: btn, x: x, y: y, press: b[end] == 'M'}, end + 1
				}
				return key{}, end + 1
			}
			end := 2
			for end < len(b) && (b[end] < 0x40 || b[end] > 0x7e) {
				end++
			}
			if end >= len(b) {
				return key{}, len(b)
			}
			seq := string(b[2 : end+1])
			names := map[string]string{"A": "up", "B": "down", "C": "right", "D": "left", "H": "home", "F": "end",
				"1~": "home", "4~": "end", "5~": "pgup", "6~": "pgdown", "3~": "delete", "Z": "shift+tab", "Q": "f2", "12~": "f2"}
			return key{name: names[seq]}, end + 1
		}
		return key{name: "alt+" + string(b[1])}, 2
	}
	switch c {
	case 0x03:
		return key{name: "ctrl+c"}, 1
	case 0x04:
		return key{name: "ctrl+d"}, 1
	case 0x15:
		return key{name: "ctrl+u"}, 1
	case 0x06:
		return key{name: "ctrl+f"}, 1
	case 0x10:
		return key{name: "ctrl+p"}, 1
	case '\r', '\n':
		return key{name: "enter"}, 1
	case 0x7f, 0x08:
		return key{name: "backspace"}, 1
	case '\t':
		return key{name: "tab"}, 1
	}
	if c < 0x20 {
		return key{}, 1
	}
	r := []rune(string(b))
	if len(r) == 0 {
		return key{}, 1
	}
	s := string(r[0])
	return key{name: s, text: s}, len(s)
}

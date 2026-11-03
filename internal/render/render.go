// Package render turns entries into pretty terminal output.
package render

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/glint/internal/icons"
	"github.com/Vaspyyy/glint/internal/textw"
	"github.com/Vaspyyy/glint/internal/theme"
)

type Config struct {
	Painter   *theme.Painter
	Icons     bool
	Classify  bool
	Bytes     bool
	TimeStyle string // relative, iso, long-iso, full, ls
	Octal     bool
	Inode     bool
	Links     bool
	Owner     bool
	Group     bool
	Git       bool
	Header    bool
	Hyperlink bool
	Peek      bool
	Commits   bool
	Heat      bool
	Bars      bool
	Width     int
	Height    int
	Now       time.Time
	Home      string
}

type Renderer struct {
	cfg   Config
	p     *theme.Painter
	fg    [theme.NumSlots]theme.Style
	faint theme.Style
	uid   int

	// per-listing maxima for relative columns
	maxSize int64
	maxHeat int
}

func New(cfg Config) *Renderer {
	r := &Renderer{cfg: cfg, p: cfg.Painter, uid: os.Getuid()}
	for s := range theme.NumSlots {
		r.fg[s] = cfg.Painter.Fg(s)
	}
	r.faint = r.fg[theme.Faint]
	return r
}

func (r *Renderer) SetGit(on bool) { r.cfg.Git = on }

func width(s string) int { return textw.Width(s) }

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

func Lookup(e *fsx.Entry) (icons.Icon, icons.Kind) {
	return icons.Lookup(icons.Info{
		Name: e.Name, Mode: e.Mode,
		IsDir: e.IsDir, IsLink: e.IsLink, Broken: e.Broken, Exe: e.Exec,
	})
}

// Name renders icon + colored name (+ classify indicator, + link target when full).
func (r *Renderer) Name(e *fsx.Entry, full bool) string {
	ic, kind := Lookup(e)
	var b strings.Builder
	if r.cfg.Icons {
		b.WriteString(r.fg[ic.Color].Render(ic.Glyph))
		b.WriteByte(' ')
	}
	name := r.nameStyle(e, kind).Render(e.Name)
	if r.cfg.Hyperlink {
		name = hyperlink(e.Abs, name)
	}
	b.WriteString(name)
	if r.cfg.Classify {
		b.WriteString(r.faint.Render(indicator(e)))
	}
	if full && e.IsLink {
		b.WriteString(r.faint.Render(" → "))
		switch {
		case e.Broken:
			b.WriteString(r.fg[theme.Red].Strike().Render(e.Target))
		case e.IsDir:
			b.WriteString(r.fg[theme.Blue].Render(e.Target))
		default:
			b.WriteString(r.fg[theme.Subtle].Render(e.Target))
		}
	}
	return b.String()
}

func (r *Renderer) nameStyle(e *fsx.Entry, k icons.Kind) theme.Style {
	switch {
	case e.Broken:
		return r.fg[theme.Red].Strike()
	case e.IsLink && e.IsDir:
		return r.fg[theme.Cyan].Bold().Italic()
	case e.IsLink:
		return r.fg[theme.Cyan].Italic()
	}
	switch k {
	case icons.Dir:
		return r.fg[theme.Blue].Bold()
	case icons.Exec:
		return r.fg[theme.Green].Bold()
	case icons.Image:
		return r.fg[theme.Pink]
	case icons.Video:
		return r.fg[theme.Purple]
	case icons.Audio:
		return r.fg[theme.Teal]
	case icons.Archive:
		return r.fg[theme.Red]
	case icons.Temp:
		return r.faint
	case icons.Special:
		return r.fg[theme.Yellow]
	}
	return r.fg[theme.Text]
}

func indicator(e *fsx.Entry) string {
	switch {
	case e.IsLink:
		return "@"
	case e.IsDir:
		return "/"
	case e.Mode&fs.ModeNamedPipe != 0:
		return "|"
	case e.Mode&fs.ModeSocket != 0:
		return "="
	case e.Exec:
		return "*"
	}
	return ""
}

func hyperlink(abs, text string) string {
	u := url.URL{Scheme: "file", Path: abs}
	return "\x1b]8;;" + u.String() + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

func (r *Renderer) perms(e *fsx.Entry) string {
	m := e.Mode
	var b strings.Builder
	t, ts := ".", r.faint
	switch {
	case m&fs.ModeDir != 0:
		t, ts = "d", r.fg[theme.Blue].Bold()
	case m&fs.ModeSymlink != 0:
		t, ts = "l", r.fg[theme.Cyan]
	case m&fs.ModeNamedPipe != 0:
		t, ts = "p", r.fg[theme.Yellow]
	case m&fs.ModeSocket != 0:
		t, ts = "s", r.fg[theme.Purple]
	case m&fs.ModeCharDevice != 0:
		t, ts = "c", r.fg[theme.Yellow]
	case m&fs.ModeDevice != 0:
		t, ts = "b", r.fg[theme.Yellow]
	}
	b.WriteString(ts.Render(t))

	special := [3]bool{m&fs.ModeSetuid != 0, m&fs.ModeSetgid != 0, m&fs.ModeSticky != 0}
	for i := range 9 {
		on := m.Perm()&(1<<(8-i)) != 0
		ch, st := "-", r.faint
		if on {
			switch i % 3 {
			case 0:
				ch, st = "r", r.fg[theme.Yellow]
			case 1:
				ch, st = "w", r.fg[theme.Red]
			case 2:
				ch, st = "x", r.fg[theme.Green]
			}
			if i < 3 {
				st = st.Bold()
			}
		}
		if i%3 == 2 && special[i/3] {
			letter := "s"
			if i == 8 {
				letter = "t"
			}
			if !on {
				letter = strings.ToUpper(letter)
			}
			ch, st = letter, r.fg[theme.Purple].Bold()
		}
		b.WriteString(st.Render(ch))
	}
	return b.String()
}

func octal(m fs.FileMode) string {
	v := uint32(m.Perm())
	if m&fs.ModeSetuid != 0 {
		v |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		v |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		v |= 0o1000
	}
	return fmt.Sprintf("%04o", v)
}

var units = []string{"B", "KB", "MB", "GB", "TB", "PB"}

func humanSize(n int64) (string, int) {
	if n < 1024 {
		return strconv.FormatInt(n, 10), 0
	}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if f < 10 {
		return strconv.FormatFloat(f, 'f', 1, 64), i
	}
	return strconv.FormatFloat(f, 'f', 0, 64), i
}

func (r *Renderer) sizeStyle(unit int) theme.Style {
	switch unit {
	case 0:
		return r.fg[theme.Muted]
	case 1:
		return r.fg[theme.Green]
	case 2:
		return r.fg[theme.Yellow].Bold()
	case 3:
		return r.fg[theme.Orange].Bold()
	}
	return r.fg[theme.Red].Bold()
}

func (r *Renderer) sizeText(n int64) string {
	num, u := humanSize(n)
	if r.cfg.Bytes {
		return r.sizeStyle(u).Render(groupDigits(n))
	}
	return r.sizeStyle(u).Render(num) + " " + r.faint.Render(fmt.Sprintf("%-2s", units[u]))
}

func (r *Renderer) size(e *fsx.Entry) string {
	switch {
	case e.HasDirSize:
		return r.sizeText(e.DirSize)
	case e.IsDir || e.Mode&(fs.ModeDevice|fs.ModeNamedPipe|fs.ModeSocket) != 0:
		return r.faint.Render("-")
	}
	return r.sizeText(e.Size)
}

func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

const day = 24 * time.Hour

func (r *Renderer) ageStyle(age time.Duration) theme.Style {
	switch {
	case age < time.Hour:
		return r.fg[theme.Green].Bold()
	case age < day:
		return r.fg[theme.Green]
	case age < 7*day:
		return r.fg[theme.Teal]
	case age < 30*day:
		return r.fg[theme.Blue]
	case age < 365*day:
		return r.fg[theme.Subtle]
	}
	return r.fg[theme.Muted]
}

func (r *Renderer) when(t time.Time) string {
	age := r.cfg.Now.Sub(t)
	st := r.ageStyle(age)
	switch r.cfg.TimeStyle {
	case "iso":
		return st.Render(t.Format("2006-01-02 15:04"))
	case "long-iso":
		return st.Render(t.Format("2006-01-02 15:04:05"))
	case "full":
		return st.Render(t.Format("Mon Jan _2 15:04:05 2006"))
	case "ls":
		if age > -time.Hour && age < 182*day {
			return st.Render(t.Format("Jan _2 15:04"))
		}
		return st.Render(t.Format("Jan _2  2006"))
	}
	return st.Render(relative(age))
}

func relative(d time.Duration) string {
	days := int(d / day)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < day:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case days < 7:
		return fmt.Sprintf("%dd ago", days)
	case days < 30:
		return fmt.Sprintf("%dw ago", days/7)
	case days < 365:
		return fmt.Sprintf("%dmo ago", days/30)
	}
	return fmt.Sprintf("%dy ago", days/365)
}

func (r *Renderer) gitChar(c byte) string {
	switch c {
	case 0, ' ':
		return r.faint.Render("-")
	case 'M':
		return r.fg[theme.Yellow].Render("M")
	case 'A':
		return r.fg[theme.Green].Render("A")
	case 'D':
		return r.fg[theme.Red].Render("D")
	case 'R', 'C':
		return r.fg[theme.Purple].Render(string(c))
	case 'T':
		return r.fg[theme.Orange].Render("T")
	case 'U':
		return r.fg[theme.Red].Bold().Render("U")
	case '?':
		return r.fg[theme.Teal].Render("?")
	case '!':
		return r.faint.Render("!")
	}
	return string(c)
}

func (r *Renderer) gitLong(s git.Status) string { return r.gitChar(s.X) + r.gitChar(s.Y) }

func (r *Renderer) gitShort(s git.Status) string {
	c := s.X
	if c == 0 || c == ' ' {
		c = s.Y
	}
	if c == 0 || c == ' ' {
		return " "
	}
	return r.gitChar(c)
}

func (r *Renderer) user(e *fsx.Entry) string {
	switch {
	case e.Sys.UID == 0:
		return r.fg[theme.Red].Render(e.Sys.Owner)
	case int(e.Sys.UID) == r.uid:
		return r.fg[theme.Yellow].Render(e.Sys.Owner)
	}
	return r.fg[theme.Subtle].Render(e.Sys.Owner)
}

func (r *Renderer) group(e *fsx.Entry) string {
	if e.Sys.GID == 0 {
		return r.fg[theme.Red].Render(e.Sys.Group)
	}
	return r.fg[theme.Muted].Render(e.Sys.Group)
}

// ErrMessage strips the "op path:" noise from fs errors.
func ErrMessage(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func (r *Renderer) errText(err error) string {
	return r.fg[theme.Red].Render("⚠ " + ErrMessage(err))
}

func (r *Renderer) PrettyPath(abs string) string {
	h := r.cfg.Home
	if h != "" && h != "/" && strings.HasPrefix(abs, h) && (len(abs) == len(h) || abs[len(h)] == '/') {
		return "~" + abs[len(h):]
	}
	return abs
}

// Title is the header line for a directory: path plus git branch/state.
func (r *Renderer) Title(e *fsx.Entry, repo *git.Repo) string {
	var b strings.Builder
	if r.cfg.Icons {
		b.WriteString(r.fg[theme.Blue].Render("\uf07c "))
	}
	b.WriteString(r.p.Gradient(r.PrettyPath(e.Abs), theme.Blue, theme.Purple, true))
	if repo == nil {
		return b.String()
	}
	b.WriteString(r.faint.Render(" on "))
	br := repo.Branch
	if r.cfg.Icons {
		br = "\ue725 " + br
	}
	b.WriteString(r.fg[theme.Purple].Bold().Render(br))
	if repo.Ahead > 0 {
		b.WriteString(r.fg[theme.Green].Render(fmt.Sprintf(" ↑%d", repo.Ahead)))
	}
	if repo.Behind > 0 {
		b.WriteString(r.fg[theme.Red].Render(fmt.Sprintf(" ↓%d", repo.Behind)))
	}
	staged, modified, untracked := repo.Counts()
	if staged+modified+untracked == 0 {
		b.WriteString(r.fg[theme.Green].Render(" ✓"))
	}
	if staged > 0 {
		b.WriteString(r.fg[theme.Green].Render(fmt.Sprintf(" +%d", staged)))
	}
	if modified > 0 {
		b.WriteString(r.fg[theme.Yellow].Render(fmt.Sprintf(" ~%d", modified)))
	}
	if untracked > 0 {
		b.WriteString(r.fg[theme.Teal].Render(fmt.Sprintf(" ?%d", untracked)))
	}
	return b.String()
}

func (r *Renderer) Empty() string { return r.faint.Italic().Render("  ∅ empty") }

// Summary is the "3 dirs · 12 files · 4.2 MB" footer.
func (r *Renderer) Summary(es []*fsx.Entry) string {
	var dirs, files int
	var total int64
	var walk func([]*fsx.Entry)
	walk = func(list []*fsx.Entry) {
		for _, e := range list {
			if e.Name == "." || e.Name == ".." {
				continue
			}
			switch {
			case e.IsDir:
				dirs++
				if e.Children != nil {
					walk(e.Children)
				} else if e.HasDirSize {
					total += e.DirSize
				}
			default:
				files++
				total += e.Size
			}
		}
	}
	walk(es)
	plural := func(n int, one, many string) string {
		if n == 1 {
			return one
		}
		return many
	}
	dot := r.faint.Render(" · ")
	return r.fg[theme.Blue].Bold().Render(strconv.Itoa(dirs)) + r.faint.Render(plural(dirs, " dir", " dirs")) + dot +
		r.fg[theme.Green].Bold().Render(strconv.Itoa(files)) + r.faint.Render(plural(files, " file", " files")) + dot +
		r.sizeText(total)
}

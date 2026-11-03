package tui

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/Vaspyyy/glint/internal/theme"
)

// ------------------------------------------------------------------ prompt

// prompt is a one-line input (or a y/n question) shown in the footer.
type prompt struct {
	label   string
	value   []rune
	confirm bool // y/n question instead of text input
	submit  func(v string)
}

func (m *model) ask(label, initial string, submit func(string)) {
	m.prompt = &prompt{label: label, value: []rune(initial), submit: submit}
}

func (m *model) confirmThen(question string, yes func()) {
	m.prompt = &prompt{label: question, confirm: true, submit: func(string) { yes() }}
}

func (m *model) handlePrompt(k key) action {
	p := m.prompt
	if p.confirm {
		switch k.name {
		case "y", "Y", "enter":
			m.prompt = nil
			p.submit("")
		case "ctrl+c":
			return actQuitNoCD
		default:
			m.prompt = nil
			m.msg = "cancelled"
		}
		return actNone
	}
	switch k.name {
	case "esc":
		m.prompt = nil
		m.msg = "cancelled"
	case "ctrl+c":
		return actQuitNoCD
	case "enter":
		m.prompt = nil
		if v := strings.TrimSpace(string(p.value)); v != "" {
			p.submit(v)
		}
	case "backspace":
		if len(p.value) > 0 {
			p.value = p.value[:len(p.value)-1]
		}
	case "ctrl+u":
		p.value = nil
	default:
		if k.text != "" {
			p.value = append(p.value, []rune(k.text)...)
		}
	}
	return actNone
}

func (m *model) promptLine() string {
	p := m.prompt
	label := m.p.Fg(theme.Purple).Bold().Render(" " + p.label + " ")
	if p.confirm {
		return label + m.p.Fg(theme.Faint).Render("y / n")
	}
	return label + m.p.Fg(theme.Text).Render(string(p.value)) + m.p.Fg(theme.Purple).Render("▏") +
		m.p.Fg(theme.Faint).Render("   enter ok · esc cancel")
}

// ------------------------------------------------------------------ targets

// targets are the selected entries, or the one under the cursor.
func (m *model) targets() []string {
	var out []string
	for p := range m.selected {
		out = append(out, p)
	}
	sort.Strings(out)
	if len(out) == 0 {
		if e := m.current(); e != nil && e.Name != ".." {
			out = []string{e.Abs}
		}
	}
	return out
}

func describe(paths []string) string {
	if len(paths) == 1 {
		return filepath.Base(paths[0])
	}
	return fmt.Sprintf("%d items", len(paths))
}

func (m *model) fail(err error) {
	var pe *fs.PathError
	var le *os.LinkError
	switch {
	case errors.As(err, &pe):
		m.msg = "⚠ " + pe.Err.Error() + ": " + filepath.Base(pe.Path)
	case errors.As(err, &le):
		m.msg = "⚠ " + le.Err.Error()
	default:
		m.msg = "⚠ " + err.Error()
	}
}

// ------------------------------------------------------------------ actions

func (m *model) rename() {
	e := m.current()
	if e == nil || e.Name == ".." {
		return
	}
	m.ask("Rename to:", e.Name, func(name string) {
		if strings.ContainsRune(name, '/') {
			m.msg = "⚠ a name can't contain /"
			return
		}
		dst := filepath.Join(m.dir, name)
		if _, err := os.Lstat(dst); err == nil {
			m.msg = "⚠ " + name + " already exists"
			return
		}
		if err := os.Rename(e.Abs, dst); err != nil {
			m.fail(err)
			return
		}
		m.msg = "renamed to " + name
		m.load(m.dir, name)
	})
}

func (m *model) trash() {
	paths := m.targets()
	if len(paths) == 0 {
		return
	}
	m.confirmThen("Move "+describe(paths)+" to the trash?", func() {
		args := append([]string{"trash", "--"}, paths...)
		if _, err := exec.Command("gio", args...).CombinedOutput(); err != nil {
			// e.g. tmpfs, USB sticks, network mounts: no trash there
			m.confirmThen("No trash here. Delete "+describe(paths)+" PERMANENTLY?", func() {
				for _, p := range paths {
					if err := os.RemoveAll(p); err != nil {
						m.fail(err)
						m.reload()
						return
					}
				}
				clear(m.selected)
				m.msg = "deleted " + describe(paths) + " permanently"
				m.reload()
			})
			return
		}
		clear(m.selected)
		m.msg = "moved " + describe(paths) + " to the trash (restore it from your file manager)"
		m.reload()
	})
}

type clipboard struct {
	paths []string
	cut   bool
}

func (m *model) copyCut(cut bool) {
	paths := m.targets()
	if len(paths) == 0 {
		return
	}
	m.clip = &clipboard{paths: paths, cut: cut}
	clear(m.selected)
	verb := "copied"
	if cut {
		verb = "cut"
	}
	m.msg = verb + " " + describe(paths) + ". Go somewhere and press p to paste"
}

func (m *model) paste() {
	c := m.clip
	if c == nil {
		m.msg = "nothing to paste: c copies, x cuts"
		return
	}
	dir := m.dir
	for _, src := range c.paths {
		if dir == src || strings.HasPrefix(dir, src+string(filepath.Separator)) {
			m.msg = "⚠ can't paste a folder into itself"
			return
		}
	}
	m.msg = "pasting " + describe(c.paths) + "…"
	go func() {
		var last string
		var err error
		for _, src := range c.paths {
			dst := freeName(filepath.Join(dir, filepath.Base(src)))
			if c.cut {
				err = move(src, dst)
			} else {
				err = copyTree(src, dst)
			}
			if err != nil {
				break
			}
			last = filepath.Base(dst)
		}
		m.events <- func() {
			if err != nil {
				m.fail(err)
			} else if c.cut {
				m.msg = "moved " + describe(c.paths) + " here"
				m.clip = nil
			} else {
				m.msg = "pasted " + describe(c.paths)
			}
			if m.dir == dir {
				m.load(dir, last)
			}
		}
	}()
}

func (m *model) newEntry(folder bool) {
	label := "New file:"
	if folder {
		label = "New folder:"
	}
	m.ask(label, "", func(name string) {
		p := filepath.Join(m.dir, name)
		var err error
		if folder {
			err = os.MkdirAll(p, 0o755)
		} else {
			if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
				var f *os.File
				if f, err = os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644); err == nil {
					f.Close()
				}
			}
		}
		if err != nil {
			m.fail(err)
			return
		}
		m.msg = "created " + name
		first, _, _ := strings.Cut(name, "/")
		m.load(m.dir, first)
	})
}

// freeName returns p, or "name (copy).ext", "name (copy 2).ext", … if taken.
func freeName(p string) string {
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	dir, base := filepath.Split(p)
	ext := filepath.Ext(base)
	if st, err := os.Lstat(p); err == nil && st.IsDir() {
		ext = ""
	}
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		suffix := " (copy)"
		if i > 1 {
			suffix = fmt.Sprintf(" (copy %d)", i)
		}
		cand := filepath.Join(dir, stem+suffix+ext)
		if _, err := os.Lstat(cand); err != nil {
			return cand
		}
	}
}

func move(src, dst string) error {
	err := os.Rename(src, dst)
	if errors.Is(err, syscall.EXDEV) { // different filesystem: copy, then remove
		if err = copyTree(src, dst); err == nil {
			err = os.RemoveAll(src)
		}
	}
	return err
}

// copyTree copies files, folders and symlinks, keeping permissions.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case !info.Mode().IsRegular():
			return nil // sockets, devices: skip
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err = io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

// ------------------------------------------------------------------ editor

// editAt opens path in the editor, at a line if the editor understands it.
func (m *model) editAt(path string, line int) action {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		for _, c := range []string{"nvim", "vim", "hx", "micro", "nano", "vi"} {
			if _, err := exec.LookPath(c); err == nil {
				editor = c
				break
			}
		}
	}
	if editor == "" {
		m.msg = "no $EDITOR set"
		return actNone
	}
	args := strings.Fields(editor)
	if line > 0 {
		switch filepath.Base(args[0]) {
		case "hx", "helix", "zed":
			path = fmt.Sprintf("%s:%d", path, line)
		case "code", "codium", "cursor":
			args = append(args, "-g")
			path = fmt.Sprintf("%s:%d", path, line)
		default: // vim, nvim, nano, micro, emacs, kak all take +N
			args = append(args, fmt.Sprintf("+%d", line))
		}
	}
	editTarget.cmd = append(args, path)
	return actEdit
}

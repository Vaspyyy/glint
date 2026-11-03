package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/Vaspyyy/glint/internal/theme"
)

// watchLoop redraws the listing whenever something in the watched
// directories changes (and every 30s so relative times stay fresh).
func (a *app) watchLoop() int {
	if !a.isTTY {
		fmt.Fprintln(a.errOut, "glint: --watch needs a terminal")
		return 2
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintln(a.errOut, "glint:", err)
		return 1
	}
	defer w.Close()

	paths := a.flags.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			w.Add(filepath.Dir(p))
			continue
		}
		w.Add(p)
		if a.o.tree {
			depth := strings.Count(filepath.Clean(p), string(filepath.Separator))
			filepath.WalkDir(p, func(sub string, d fs.DirEntry, err error) error {
				if err != nil || !d.IsDir() || sub == p {
					return nil
				}
				if d.Name() == ".git" || d.Name() == "node_modules" ||
					(a.o.level > 0 && strings.Count(sub, string(filepath.Separator))-depth >= a.o.level) {
					return filepath.SkipDir
				}
				w.Add(sub)
				return nil
			})
		}
	}

	out := os.Stdout
	out.WriteString("\x1b[?1049h\x1b[?25l")
	defer out.WriteString("\x1b[?25h\x1b[?1049l")

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(sigs)
	defer signal.Stop(winch)

	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	changes := 0
	var debounce <-chan time.Time
	var lastEvent string

	draw := func() {
		var buf bytes.Buffer
		width := a.termWidth()
		a.list(&buf, width)
		p := a.painter
		status := p.Fg(theme.Green).Render(" ◉ ") + p.Fg(theme.Subtle).Render("watching") +
			p.Fg(theme.Faint).Render(" · "+time.Now().Format("15:04:05"))
		if changes > 0 {
			status += p.Fg(theme.Faint).Render(" · ") + p.Fg(theme.Yellow).Render(fmt.Sprintf("%d changes", changes))
		}
		if lastEvent != "" {
			status += p.Fg(theme.Faint).Render(" · last: ") + p.Fg(theme.Pink).Render(lastEvent)
		}
		status += p.Fg(theme.Faint).Render(" · ctrl+c to quit")
		frame := strings.ReplaceAll(buf.String(), "\n", "\x1b[K\n")
		out.WriteString("\x1b[?2026h\x1b[H" + status + "\x1b[K\n\x1b[K\n" + frame + "\x1b[J\x1b[?2026l")
	}
	draw()
	for {
		select {
		case <-sigs:
			return 0
		case <-winch:
			draw()
		case <-tick.C:
			draw()
		case ev, ok := <-w.Events:
			if !ok {
				return 0
			}
			if ev.Has(fsnotify.Chmod) {
				continue
			}
			changes++
			lastEvent = filepath.Base(ev.Name)
			if ev.Has(fsnotify.Create) {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() && a.o.tree {
					w.Add(ev.Name)
				}
			}
			if debounce == nil {
				debounce = time.After(120 * time.Millisecond)
			}
		case <-debounce:
			debounce = nil
			draw()
		case <-w.Errors:
		}
	}
}

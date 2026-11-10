//go:build unix

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
	"github.com/Vaspyyy/glint/internal/render"
	"github.com/Vaspyyy/glint/internal/theme"
	"github.com/muesli/termenv"
)

func TestPreviewsLeavePipesAlone(t *testing.T) {
	dir := t.TempDir()
	for _, p := range pipetest.Both(t, dir, ".zip") {
		pipetest.Returns(t, "zipList "+p, func() { zipList(p, 10) })
		pipetest.Returns(t, "tarList "+p, func() { tarList(p, false, 10); tarList(p, true, 10) })
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	pipetest.Returns(t, "copyTree", func() {
		if err := copyTree(dir, dst); err != nil {
			t.Errorf("copyTree: %v", err)
		}
	})
	if data, err := os.ReadFile(filepath.Join(dst, "file.txt")); err != nil || string(data) != "text\n" {
		t.Errorf("the copy's file.txt: %q, %v", data, err)
	}
}

// The list of recent folders is read and written when the TUI starts: a
// pipe in its place must not keep it from starting, and stays what it is.
func TestHistoryLeavesAPipeAlone(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	recent := filepath.Join(state, "glint", "recent")
	pipetest.Pipe(t, recent)
	histOnce, history = sync.Once{}, nil
	t.Cleanup(func() { histOnce, history = sync.Once{}, nil })
	pipetest.Returns(t, "loadHistory", loadHistory)
	if len(history) != 0 {
		t.Errorf("history read from a pipe: %q", history)
	}
	pipetest.Returns(t, "remember", func() { remember(t.TempDir()) })
	if st, err := os.Lstat(recent); err != nil || st.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("the pipe is no pipe any more: %v", err)
	}
}

// A link shows what it leads to by its name (an archive's list), and no
// text: that's what following it is for.
func TestPreviewOfALink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("the text of the notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("notes.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	p := theme.NewPainter(termenv.Ascii, theme.Palettes["catppuccin"])
	m := &model{p: p, r: render.New(render.Config{Painter: p}), now: time.Now()}
	for name, want := range map[string]bool{"notes.txt": true, "link.txt": false} {
		e, err := fsx.Stat(filepath.Join(dir, name), fsx.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(strings.Join(m.preview(e, 60, 20), "\n"), "the text of the notes"); got != want {
			t.Errorf("the preview of %s shows the text: %v, want %v", name, got, want)
		}
	}
}

// A pipe has nothing to read, whatever its name says it is: its preview
// is the two lines about it, with no archive list, image or error below.
func TestPreviewOfAPipe(t *testing.T) {
	dir := t.TempDir()
	p := theme.NewPainter(termenv.Ascii, theme.Palettes["catppuccin"])
	m := &model{p: p, r: render.New(render.Config{Painter: p}), now: time.Now()}
	for _, ext := range []string{".zip", ".tar", ".png", ".txt"} {
		for _, path := range pipetest.Both(t, dir, ext) {
			e, err := fsx.Stat(path, fsx.Options{})
			if err != nil {
				t.Fatal(err)
			}
			var lines []string
			pipetest.Returns(t, "preview "+path, func() { lines = m.preview(e, 60, 20) })
			if len(lines) != 3 {
				t.Errorf("the preview of %s has %d lines, want the 3 above the text:\n%s", path, len(lines), strings.Join(lines, "\n"))
			}
		}
	}
}

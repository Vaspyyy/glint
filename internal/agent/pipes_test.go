//go:build unix

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

func TestReadersLeavePipesAlone(t *testing.T) {
	dir := t.TempDir()
	for _, p := range pipetest.Both(t, dir, ".go") {
		pipetest.Returns(t, "Outline "+p, func() {
			if syms := Outline(p, 10); len(syms) > 0 {
				t.Errorf("Outline(%s) = %v", p, syms)
			}
		})
		pipetest.Returns(t, "looksText "+p, func() { looksText(p) })
		pipetest.Returns(t, "countLines "+p, func() {
			if _, _, err := countLines(p); err == nil {
				t.Errorf("countLines(%s) read a pipe", p)
			}
		})
	}
	pipetest.Pipe(t, filepath.Join(dir, "README.md"))
	pipetest.Returns(t, "readmeSummary", func() {
		if s := readmeSummary(dir); s != "" {
			t.Errorf("readmeSummary = %q for a pipe", s)
		}
	})
}

func TestCountLines(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct {
		data string
		want int
		open bool
	}{
		"empty":   {"", 0, false},
		"three":   {"a\nb\nc\n", 3, false},
		"no-end":  {"a\nb", 1, true},
		"binary":  {"a\n\x00b\n", 0, false},
		"big":     {strings.Repeat("line\n", 200_000), 200_000, false},
		"big-end": {strings.Repeat("line\n", 200_000) + "x", 200_000, true},
		"late-00": {strings.Repeat("line\n", 2000) + "\x00\n", 2001, false},
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(c.data), 0o644); err != nil {
			t.Fatal(err)
		}
		if n, open, err := countLines(p); err != nil || n != c.want || open != c.open {
			t.Errorf("countLines(%s) = %d, %v, %v; want %d, %v", name, n, open, err, c.want, c.open)
		}
	}
}

// A file that goes on and on isn't read to its end.
func TestCountLinesGivesUp(t *testing.T) {
	old := maxCounted
	maxCounted = 1 << 20
	t.Cleanup(func() { maxCounted = old })
	dir := t.TempDir()
	for name, c := range map[string]struct{ size, want int }{
		"under": {1 << 20, 1 << 20 / 8},
		"over":  {1<<20 + 8, 0},
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(strings.Repeat("a line\n\n", c.size/8)), 0o644); err != nil {
			t.Fatal(err)
		}
		if n, _, err := countLines(p); err != nil || n != 2*c.want {
			t.Errorf("countLines(%s) = %d, %v; want %d", name, n, err, 2*c.want)
		}
	}
}

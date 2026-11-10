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
			if _, err := countLines(p); err == nil {
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
	}{
		"empty":   {"", 0},
		"three":   {"a\nb\nc\n", 3},
		"no-end":  {"a\nb", 1},
		"binary":  {"a\n\x00b\n", 0},
		"big":     {strings.Repeat("line\n", 200_000), 200_000},
		"late-00": {strings.Repeat("line\n", 2000) + "\x00\n", 2001},
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(c.data), 0o644); err != nil {
			t.Fatal(err)
		}
		if n, err := countLines(p); err != nil || n != c.want {
			t.Errorf("countLines(%s) = %d, %v; want %d", name, n, err, c.want)
		}
	}
}

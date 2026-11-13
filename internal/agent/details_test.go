package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file can hold more than its size said: one that grew since the scan,
// one in /proc that reports no size. Its lines are counted all the same.
func TestDetailsOfAFileLongerThanItsSize(t *testing.T) {
	dir := t.TempDir()
	lines := maxLineCount/5 + 3
	for name, c := range map[string]struct {
		end  string
		want int
	}{
		"grown.txt": {"", lines},
		"open.txt":  {"no line break", lines + 1},
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(strings.Repeat("line\n", lines)+c.end), 0o644); err != nil {
			t.Fatal(err)
		}
		f := &node{name: name, rel: name, abs: p, mode: 0o644, size: 10}
		details(&node{dir: true, abs: dir, kids: []*node{f}}, Options{})
		if f.lines != c.want || f.binary {
			t.Errorf("%s: %d lines, binary %v; want %d lines", name, f.lines, f.binary, c.want)
		}
	}
}

// The symbols loaded for a repo the map opens are those of the files it
// outlines: a source file too large for details to outline isn't read.
func TestOutlinedFilesLeaveOutTooLargeOnes(t *testing.T) {
	small := &node{name: "small.go", abs: "/r/small.go", mode: 0o644, size: 10}
	large := &node{name: "large.go", abs: "/r/large.go", mode: 0o644, size: maxLineCount + 1}
	got := outlinedFiles(&node{dir: true, expanded: true, abs: "/r", kids: []*node{small, large}}, 50)
	if len(got) != 1 || got[0] != small.abs {
		t.Errorf("outlined: %v; want only %s", got, small.abs)
	}
}

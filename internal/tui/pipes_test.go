//go:build unix

package tui

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
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

// The list of recent folders is read when the TUI starts: a pipe in its
// place must not keep it from starting.
func TestHistoryLeavesAPipeAlone(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	pipetest.Pipe(t, filepath.Join(state, "glint", "recent"))
	histOnce, history = sync.Once{}, nil
	t.Cleanup(func() { histOnce, history = sync.Once{}, nil })
	pipetest.Returns(t, "loadHistory", loadHistory)
	if len(history) != 0 {
		t.Errorf("history read from a pipe: %q", history)
	}
}

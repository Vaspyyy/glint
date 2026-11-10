//go:build unix

package theme

import (
	"path/filepath"
	"testing"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

func TestWalPaletteLeavesAPipeAlone(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	pipetest.Pipe(t, filepath.Join(cache, "wal", "colors.json"))
	pipetest.Returns(t, "walPalette", func() {
		if _, err := walPalette(); err == nil {
			t.Error("walPalette read a pipe")
		}
	})
}

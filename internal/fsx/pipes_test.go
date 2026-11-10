//go:build unix

package fsx

import (
	"path/filepath"
	"testing"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

// A folder can be swapped for a pipe after it was listed: reading it is
// then an error, not a wait.
func TestReadDirOfAPipe(t *testing.T) {
	for _, p := range pipetest.Both(t, t.TempDir(), "") {
		pipetest.Returns(t, "ReadDir "+p, func() {
			if entries, err := ReadDir(p, Options{}); err == nil {
				t.Errorf("ReadDir(%s) = %d entries", filepath.Base(p), len(entries))
			}
		})
	}
}

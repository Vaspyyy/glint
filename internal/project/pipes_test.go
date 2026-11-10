//go:build unix

package project

import (
	"path/filepath"
	"testing"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

// A folder can be swapped for a pipe after it was listed: its names are
// then an error, not a wait.
func TestNamesOfAPipe(t *testing.T) {
	for _, p := range pipetest.Both(t, t.TempDir(), "") {
		pipetest.Returns(t, "Names "+p, func() {
			if names, err := Names(p); err == nil {
				t.Errorf("Names(%s) = %v", filepath.Base(p), names)
			}
		})
		pipetest.Returns(t, "Detect "+p, func() { Detect(p, nil) })
	}
}

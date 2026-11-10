//go:build unix

package img

import (
	"testing"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

func TestImagesLeavePipesAlone(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, p := range pipetest.Both(t, t.TempDir(), ".png") {
		pipetest.Returns(t, "Load "+p, func() {
			if _, err := Load(p); err == nil {
				t.Errorf("Load(%s) read a pipe", p)
			}
		})
		pipetest.Returns(t, "Dimensions "+p, func() {
			if _, _, err := Dimensions(p); err == nil {
				t.Errorf("Dimensions(%s) read a pipe", p)
			}
		})
		pipetest.Returns(t, "decodePNG "+p, func() { decodePNG(p) })
		pipetest.Returns(t, "Thumb "+p, func() { Thumb(p, 64) })
	}
}

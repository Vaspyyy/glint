//go:build unix

package peek

import (
	"testing"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

// Each reader is given a pipe and a link to one directly, as it would get
// them when the file was swapped after it was listed.
func TestReadersLeavePipesAlone(t *testing.T) {
	for _, p := range pipetest.Both(t, t.TempDir(), ".zip") {
		pipetest.Returns(t, "zipCount "+p, func() {
			if _, err := zipCount(p); err == nil {
				t.Errorf("zipCount(%s) read a pipe", p)
			}
		})
		pipetest.Returns(t, "tarCount "+p, func() { tarCount(p, false); tarCount(p, true) })
		pipetest.Returns(t, "pdfPages "+p, func() { pdfPages(p) })
		pipetest.Returns(t, "elfInfo "+p, func() { elfInfo(p) })
		pipetest.Returns(t, "content "+p, func() { content(&fsx.Entry{Name: "x.txt", Path: p}) })
	}
}

func TestOfSkipsPipesUnderAnyName(t *testing.T) {
	dir := t.TempDir()
	for _, ext := range []string{".zip", ".png", ".tar.gz", ".pdf", ".txt", ".mp4", ""} {
		pipetest.Both(t, dir, ext)
	}
	entries, err := fsx.ReadDir(dir, fsx.Options{})
	if err != nil || len(entries) != 14 {
		t.Fatalf("ReadDir: %d entries, %v", len(entries), err)
	}
	for _, e := range entries {
		pipetest.Returns(t, "Of "+e.Name, func() {
			if pk := Of(e); pk != nil {
				t.Errorf("Of(%s) = %+v, want nothing for a pipe", e.Name, pk)
			}
		})
	}
}

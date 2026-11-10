//go:build unix

package peek

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// ffprobe opens the file itself, so it gets a time limit, and so does
// what it started: a wrapper script's ffprobe keeps the output open after
// the script is killed.
func TestProbeGivesUp(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nsleep 30\necho late\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ffprobeOnce.Do(func() {})
	defer func(path string, limit time.Duration) { ffprobePath, probeLimit = path, limit }(ffprobePath, probeLimit)
	ffprobePath, probeLimit = shim, 300*time.Millisecond
	start := time.Now()
	pipetest.Returns(t, "probe", func() {
		if got := probe(filepath.Join(dir, "x.mp4")); got != "" {
			t.Errorf("probe = %q", got)
		}
	})
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("probe took %v with a limit of %v", d, probeLimit)
	}
}

// What ffprobe said counts when it ended well, also when something it
// started outlives it and keeps the output open.
func TestProbeOfAWrapperThatLeavesAChild(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "ffprobe")
	script := "#!/bin/sh\necho width=64\necho height=48\necho duration=2.5\n(sleep 2 &)\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ffprobeOnce.Do(func() {})
	defer func(path string, limit time.Duration) { ffprobePath, probeLimit = path, limit }(ffprobePath, probeLimit)
	ffprobePath, probeLimit = shim, 600*time.Millisecond
	pipetest.Returns(t, "probe", func() {
		if got := probe(filepath.Join(dir, "x.mp4")); !strings.Contains(got, "64×48") {
			t.Errorf("probe = %q, want the size ffprobe gave", got)
		}
	})
}

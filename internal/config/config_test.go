//go:build unix

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

type flags map[string]string

func (f flags) Set(name, value string) error { f[name] = value; return nil }

// The config file in its usual place is read on every run: a pipe there
// would hang them all. One named with --config may be a pipe, as the
// shell's <(…) makes.
func TestLoadAPipe(t *testing.T) {
	pipe := filepath.Join(t.TempDir(), "glint", "config.toml")
	pipetest.Pipe(t, pipe)
	pipetest.Returns(t, "Load of the usual file", func() {
		if err := Load(pipe, false, flags{}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("Load of a pipe in the usual place: %v, want it refused", err)
		}
	})
	got := flags{}
	go os.WriteFile(pipe, []byte("theme = \"nord\"\n"), 0o644)
	pipetest.Returns(t, "Load of a named file", func() {
		if err := Load(pipe, true, got); err != nil || got["theme"] != "nord" {
			t.Errorf("Load of a named pipe: %v, flags %v", err, got)
		}
	})
	if err := Load(filepath.Join(t.TempDir(), "none.toml"), false, flags{}); err != nil {
		t.Errorf("Load of a missing file: %v", err)
	}
}

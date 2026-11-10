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
// would hang them all. It is no config, like a link to /dev/null and a
// missing file.
func TestLoadWhatIsNoFile(t *testing.T) {
	dir := t.TempDir()
	pipe, null := filepath.Join(dir, "glint", "config.toml"), filepath.Join(dir, "null.toml")
	pipetest.Pipe(t, pipe)
	if err := os.Symlink(os.DevNull, null); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{pipe, null, filepath.Join(dir, "none.toml")} {
		got := flags{}
		pipetest.Returns(t, "Load "+p, func() {
			if err := Load(p, false, got); err != nil || len(got) != 0 {
				t.Errorf("Load(%s) in the usual place: %v, flags %v; want no config", p, err, got)
			}
		})
	}
	if err := Load(dir, false, flags{}); err == nil {
		t.Error("Load of a folder: no error")
	}
}

// A file named with --config may be a pipe, as the shell's <(…) makes.
func TestLoadANamedPipe(t *testing.T) {
	pipe := filepath.Join(t.TempDir(), "config.toml")
	pipetest.Pipe(t, pipe)
	got := flags{}
	go os.WriteFile(pipe, []byte("theme = \"nord\"\n"), 0o644)
	pipetest.Returns(t, "Load of a named file", func() {
		if err := Load(pipe, true, got); err != nil || got["theme"] != "nord" {
			t.Errorf("Load of a named pipe: %v, flags %v", err, got)
		}
	})
}

// However long the file is, its last line counts.
func TestLoadALongFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(strings.Repeat("# a line of comment\n", 300_000)+"theme = \"nord\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := flags{}
	if err := Load(p, false, got); err != nil || got["theme"] != "nord" {
		t.Errorf("Load of 6 MB: %v, flags %v", err, got)
	}
}

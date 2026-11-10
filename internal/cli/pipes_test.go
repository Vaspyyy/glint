//go:build unix

package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

// Opening a named pipe to read it waits until something writes to it. A
// pipe can have any name, so every place that reads a file for a listing
// (the README, a manifest, the largest source file, a preview, an image)
// has to leave it alone, also behind a symlink. Each view is run over a
// folder full of them and has to come back.
func TestPipesDontHangAListing(t *testing.T) {
	isolate(t)
	names := []string{
		"README.md", "go.mod", "package.json", "Cargo.toml", "pyproject.toml", "Makefile",
		"main.go", "notes.txt", "x.tar", "x.tar.gz", "x.zip", "x.pdf", "x.png", "x.svg", "run.sh", "LICENSE",
	}
	for _, kind := range []string{"pipe", "link to a pipe"} {
		for _, name := range names {
			root := t.TempDir()
			dir := filepath.Join(root, "proj")
			write(t, filepath.Join(dir, "a.go"), "package a\n\nfunc A() {}\n")
			write(t, filepath.Join(dir, "b.go"), "package a\n\nfunc B() {}\n")
			if name != "go.mod" {
				write(t, filepath.Join(dir, "go.mod"), "module a\n") // a project: its README is read
			}
			pipe := filepath.Join(dir, name)
			if kind != "pipe" {
				pipe = filepath.Join(dir, ".pipe")
				if err := os.Symlink(".pipe", filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			pipetest.Pipe(t, pipe)
			views := [][]string{
				{"-A"}, {"-A", "--outline"}, {"-A", "-l"}, {"-p"}, {"-C"}, {"-l"}, {"-lp"}, {"-T"}, {"-lcH"},
				{"--thumbs"}, {"--json"}, {"-A", name}, {"-A", "--json", name}, {"-p", name},
				{"-A", ".."}, {"-P", ".."}, {"-A", "--outline", ".."},
			}
			t.Chdir(dir)
			for _, args := range views {
				done := make(chan string)
				go func() {
					var out, errOut bytes.Buffer
					run(args, &out, &errOut)
					done <- out.String()
				}()
				select {
				case out := <-done:
					if len(args) == 1 && args[0] == "-A" && !strings.Contains(out, "a.go") {
						t.Errorf("%s named %s: glint -A doesn't list a.go:\n%s", kind, name, out)
					}
				case <-time.After(20 * time.Second):
					t.Fatalf("%s named %s: glint %s is still waiting after 20s", kind, name, strings.Join(args, " "))
				}
			}
		}
	}
}

// The config file in its usual place can be a pipe or a link to
// /dev/null: glint runs as without one. A pipe named with --config is
// read (--config <(…)).
func TestConfigThatIsNoFile(t *testing.T) {
	cfg := isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".hidden"), "")
	write(t, filepath.Join(dir, "shown"), "")
	t.Chdir(dir)
	list := func(what string, args ...string) string {
		t.Helper()
		var out, errOut bytes.Buffer
		pipetest.Returns(t, what, func() {
			if status := run(append(args, "-1"), &out, &errOut); status != 0 {
				t.Errorf("%s: status %d, %s", what, status, errOut.String())
			}
		})
		return out.String()
	}
	usual := filepath.Join(cfg, "glint", "config.toml")
	pipetest.Pipe(t, usual)
	if out := list("a pipe in the usual place"); !strings.Contains(out, "shown") || strings.Contains(out, ".hidden") {
		t.Errorf("a pipe in the usual place:\n%s", out)
	}
	if err := os.Remove(usual); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(os.DevNull, usual); err != nil {
		t.Fatal(err)
	}
	if out := list("a link to /dev/null in the usual place"); !strings.Contains(out, "shown") || strings.Contains(out, ".hidden") {
		t.Errorf("a link to /dev/null in the usual place:\n%s", out)
	}
	named := filepath.Join(t.TempDir(), "named.toml")
	pipetest.Pipe(t, named)
	go os.WriteFile(named, []byte("all = 1\n"), 0o644)
	if out := list("a pipe named with --config", "--config", named); !strings.Contains(out, ".hidden") {
		t.Errorf("--config with a pipe that says all = 1:\n%s", out)
	}
}

// The links of a series of files are counted like the files: their lines
// are part of what the series line says, and the largest are named.
func TestSeriesOfLinksHasItsLines(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	for i, name := range []string{"one.py", "two.py", "three.py", "four.py"} {
		write(t, filepath.Join(dir, "real", name), strings.Repeat("x = 1\n", 100*(i+1)))
		if err := os.Symlink(filepath.Join("real", name), filepath.Join(dir, fmt.Sprintf("lnk%d.py", i+1))); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	run([]string{"-A", "--no-git"}, &out, &errOut)
	for _, want := range []string{"lnk{1..4}.py ×4 100–400L", "lnk4.py 400L"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("glint -A doesn't say %q:\n%s", want, out.String())
		}
	}
}

// What isn't a file is no source file, whatever it's called: a pipe, and
// a series of them, aren't among the largest.
func TestLargestSourceFilesAreFiles(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.py"), strings.Repeat("x = 1\n", 10))
	write(t, filepath.Join(dir, "b.py"), strings.Repeat("x = 1\n", 20))
	for _, name := range []string{"solo.py", "p1.py", "p2.py", "p3.py"} {
		pipetest.Pipe(t, filepath.Join(dir, name))
	}
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	pipetest.Returns(t, "glint -A", func() { run([]string{"-A", "--no-git"}, &out, &errOut) })
	want := "largest source files (excl. vendored/generated): b.py 20L, a.py 10L\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("glint -A doesn't say %q:\n%s", want, out.String())
	}
}

//go:build unix

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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
			pipe := filepath.Join(dir, name)
			if kind != "pipe" {
				pipe = filepath.Join(dir, ".pipe")
				if err := os.Symlink(".pipe", filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			if err := syscall.Mkfifo(pipe, 0o644); err != nil {
				t.Skip("no named pipes here:", err)
			}
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

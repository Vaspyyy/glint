//go:build unix

package fsx

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// within fails the test when f takes longer than a read of a small file
// ever does: opening a named pipe the usual way waits for a writer that
// never comes.
func within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s is still waiting after 10s", what)
	}
}

func TestOpenTakesRegularFilesOnly(t *testing.T) {
	dir := t.TempDir()
	file, pipe := filepath.Join(dir, "file"), filepath.Join(dir, "pipe")
	if err := os.WriteFile(file, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(pipe, 0o644); err != nil {
		t.Skip("no named pipes here:", err)
	}
	for link, target := range map[string]string{"to-file": "file", "to-pipe": "pipe", "to-dir": ".", "to-nothing": "gone"} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{
		"file": "one\ntwo\n", "to-file": "one\ntwo\n",
		"pipe": "", "to-pipe": "", ".": "", "to-dir": "", "to-nothing": "", "gone": "", os.DevNull: "",
	} {
		path := name
		if !filepath.IsAbs(name) {
			path = filepath.Join(dir, name)
		}
		within(t, "ReadFile "+name, func() {
			data, err := ReadFile(path, 0)
			if (err == nil) != (want != "") || string(data) != want {
				t.Errorf("ReadFile(%s) = %q, %v; want %q", name, data, err, want)
			}
		})
	}
	if data, err := ReadFile(file, 5); err != nil || string(data) != "one\nt" {
		t.Errorf("ReadFile(file, 5) = %q, %v; want the first 5 bytes", data, err)
	}
}

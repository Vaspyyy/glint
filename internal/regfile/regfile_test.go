//go:build unix

package regfile

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

func TestOpenTakesRegularFilesOnly(t *testing.T) {
	dir := t.TempDir()
	file, pipe := filepath.Join(dir, "file"), filepath.Join(dir, "pipe")
	if err := os.WriteFile(file, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pipetest.Pipe(t, pipe)
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
		pipetest.Returns(t, "Read "+name, func() {
			data, err := Read(path, 0)
			if (err == nil) != (want != "") || string(data) != want {
				t.Errorf("Read(%s) = %q, %v; want %q", name, data, err, want)
			}
		})
		if got := Is(path); got != (want != "") {
			t.Errorf("Is(%s) = %v", name, got)
		}
	}
	if data, err := Read(file, 5); err != nil || string(data) != "one\nt" {
		t.Errorf("Read(file, 5) = %q, %v; want the first 5 bytes", data, err)
	}
	// what's missing is said the way os.Open says it
	if _, err := Open(filepath.Join(dir, "gone")); !os.IsNotExist(err) || err.Error() != "open "+filepath.Join(dir, "gone")+": no such file or directory" {
		t.Errorf("Open of a missing file: %v", err)
	}
}

// A process that waits to write to a pipe is let through by whoever opens
// the pipe to read it, and killed (SIGPIPE) when that reader closes it
// again. Looking at the path first leaves it waiting.
func TestAWaitingWriterIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	pipe := filepath.Join(dir, "pipe")
	pipetest.Pipe(t, pipe)
	if err := os.Symlink("pipe", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	wrote := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(pipe, os.O_WRONLY, 0) // waits for a reader
		if err == nil {
			_, err = f.Write([]byte("x"))
			f.Close()
		}
		wrote <- err
	}()
	time.Sleep(200 * time.Millisecond) // the writer is waiting now
	for _, p := range []string{pipe, filepath.Join(dir, "link")} {
		pipetest.Returns(t, "the readers", func() {
			if f, err := Open(p); err == nil {
				f.Close()
				t.Errorf("Open(%s) opened a pipe", p)
			}
			Read(p, 0)
			Is(p)
			if f, err := OpenDir(p); err == nil {
				f.Close()
				t.Errorf("OpenDir(%s) opened a pipe", p)
			}
		})
	}
	select {
	case err := <-wrote:
		t.Fatalf("the writer was let through: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	// let it go: a reader that stays
	r, err := os.OpenFile(pipe, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := <-wrote; err != nil {
		t.Errorf("the writer, with a reader: %v", err)
	}
}

// Files in /proc say they are empty and aren't: all of one is read, and
// no more than the limit.
func TestReadMoreThanTheSizeSays(t *testing.T) {
	const path = "/proc/cpuinfo"
	want, err := os.ReadFile(path)
	if st, serr := os.Stat(path); err != nil || serr != nil || st.Size() != 0 || len(want) < 2000 {
		t.Skip("no /proc/cpuinfo of size 0 with 2000 bytes in it here")
	}
	// the numbers in it change between two reads: the lines don't
	if data, err := Read(path, 0); err != nil || bytes.Count(data, []byte{'\n'}) != bytes.Count(want, []byte{'\n'}) {
		t.Errorf("Read(%s) = %d bytes, %v; os.ReadFile has %d", path, len(data), err, len(want))
	}
	if data, err := Read(path, 1500); err != nil || len(data) != 1500 {
		t.Errorf("Read(%s, 1500) = %d bytes, %v", path, len(data), err)
	}
}

func TestOpenDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), nil, 0o644)
	f, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil || len(names) != 1 || names[0] != "a" {
		t.Errorf("the folder's names: %v, %v", names, err)
	}
	for _, p := range append(pipetest.Both(t, dir, ""), filepath.Join(dir, "a"), filepath.Join(dir, "gone")) {
		pipetest.Returns(t, "OpenDir "+p, func() {
			if f, err := OpenDir(p); err == nil {
				f.Close()
				t.Errorf("OpenDir(%s) opened what isn't a folder", p)
			}
		})
	}
}

// Write replaces a regular file, through a link too, and makes a new one.
// A pipe in its place stays a pipe, with or without a reader.
func TestWrite(t *testing.T) {
	dir := t.TempDir()
	file, link, fresh := filepath.Join(dir, "file"), filepath.Join(dir, "link"), filepath.Join(dir, "new")
	os.WriteFile(file, []byte("a long line of old text\n"), 0o644)
	os.Symlink("file", link)
	for _, p := range []string{file, link, fresh} {
		if err := Write(p, []byte("new\n"), 0o644); err != nil {
			t.Errorf("Write(%s): %v", p, err)
		}
		if data, _ := os.ReadFile(p); string(data) != "new\n" {
			t.Errorf("%s holds %q", p, data)
		}
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link is no link any more: %v", err)
	}
	pipes := pipetest.Both(t, dir, ".txt")
	r, err := os.OpenFile(pipes[0], os.O_RDONLY|syscall.O_NONBLOCK, 0) // a reader, for the second round
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, p := range pipes {
		pipetest.Returns(t, "Write "+p, func() {
			if err := Write(p, []byte("new\n"), 0o644); err == nil {
				t.Errorf("Write(%s) wrote into a pipe", p)
			}
		})
		if st, err := os.Stat(p); err != nil || st.Mode()&os.ModeNamedPipe == 0 {
			t.Errorf("%s is no pipe any more: %v", p, err)
		}
	}
	if n, _ := r.Read(make([]byte, 8)); n != 0 {
		t.Errorf("the pipe's reader got %d bytes", n)
	}
}

package regfile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

// A link that turns from a file into a pipe while Open looks at it doesn't
// get the pipe opened: the process waiting to write to it keeps waiting,
// instead of being let through and killed when the reader closes it.
func TestAPipeSwappedInIsLeftAlone(t *testing.T) {
	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skip("no /proc here: Open looks and opens in two steps")
	}
	dir := t.TempDir()
	file, pipe, link := filepath.Join(dir, "file"), filepath.Join(dir, "pipe"), filepath.Join(dir, "link")
	if err := os.WriteFile(file, []byte("text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pipetest.Pipe(t, pipe)
	if err := os.Symlink("file", link); err != nil {
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

	// the link points at the file and at the pipe in turn, as fast as it can
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		next := link + ".next"
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			os.Remove(next)
			os.Symlink([]string{"file", "pipe"}[i%2], next)
			os.Rename(next, link)
		}
	}()
	pipetest.Returns(t, "the reads", func() {
		for range 20000 {
			if f, err := Open(link); err == nil {
				if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
					t.Errorf("Open gave what isn't a regular file: %v", err)
				}
				f.Close()
			}
		}
	})
	close(stop)
	<-stopped

	select {
	case err := <-wrote:
		t.Fatalf("the writer was let through: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	r, err := os.OpenFile(pipe, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := <-wrote; err != nil {
		t.Errorf("the writer, with a reader: %v", err)
	}
}

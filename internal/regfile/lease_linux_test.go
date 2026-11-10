package regfile

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Vaspyyy/glint/internal/regfile/pipetest"
)

// A file that someone holds a lease on (Samba does) refuses an open that
// doesn't wait. It is opened again the usual way, which asks the holder
// to give the lease up and waits until it has.
func TestOpenWaitsForALease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leased")
	if err := os.WriteFile(path, []byte("text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	holder, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	asked := make(chan os.Signal, 1)
	signal.Notify(asked, syscall.SIGIO) // the kernel's request to give it up
	defer signal.Stop(asked)
	lease := func(kind uintptr) syscall.Errno {
		_, _, e := syscall.Syscall(syscall.SYS_FCNTL, holder.Fd(), syscall.F_SETLEASE, kind)
		return e
	}
	if e := lease(syscall.F_WRLCK); e != 0 {
		t.Skip("no leases here:", e)
	}
	if f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
		f.Close()
		t.Skip("an open by the lease's own process doesn't break it here")
	}
	gave := make(chan bool, 1)
	go func() {
		<-asked
		gave <- lease(syscall.F_UNLCK) == 0
	}()
	pipetest.Returns(t, "Read of a leased file", func() {
		if data, err := Read(path, 0); err != nil || string(data) != "text\n" {
			t.Errorf("Read of a leased file: %q, %v", data, err)
		}
	})
	select {
	case ok := <-gave:
		if !ok {
			t.Error("the lease wasn't given up")
		}
	default:
		t.Error("the file was read without the lease being given up")
	}
}

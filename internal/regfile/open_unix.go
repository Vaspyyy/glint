//go:build unix

package regfile

import (
	"errors"
	"os"
	"syscall"
)

// openNoWait opens path without waiting for the other end of a pipe. A
// regular file that another process holds a lease on (Samba, an NFS
// delegation) makes such an open fail instead of wait: that one is opened
// again the usual way, which waits for the lease to be given up. On Linux
// a pipe that took the path's place since Open looked at it isn't opened
// at all (see openPinned).
func openNoWait(path string) (*os.File, error) {
	if f, err, ok := openPinned(path); ok {
		return f, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.EWOULDBLOCK) && Is(path) {
		return os.Open(path)
	}
	return f, err
}

// OpenDir opens a folder to list it. A pipe that took its place since it
// was seen fails at once: O_DIRECTORY doesn't open one.
func OpenDir(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
}

// createNoWait opens path to replace its contents, creating it. A pipe
// without a reader fails (ENXIO) instead of waiting for one, and what is
// there is only cut short once it's known to be a regular file.
func createNoWait(path string, perm os.FileMode) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|syscall.O_NONBLOCK, perm)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err == nil && st.Mode().IsRegular() {
		if err := f.Truncate(0); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

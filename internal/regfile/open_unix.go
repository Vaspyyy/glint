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
// again the usual way, which waits for the lease to be given up.
func openNoWait(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.EWOULDBLOCK) && Is(path) {
		return os.Open(path)
	}
	return f, err
}

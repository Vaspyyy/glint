//go:build unix

package fsx

import (
	"os"
	"syscall"
)

// openNoWait opens a path for reading without blocking on a named pipe
// that nobody writes to.
func openNoWait(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

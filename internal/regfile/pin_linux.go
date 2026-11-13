//go:build linux

package regfile

import (
	"errors"
	"io/fs"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// openPinned opens path for reading without the moment, between Open's look
// at the path and the open, in which the path may be swapped for a pipe:
// opening a pipe, even without waiting, lets through the process waiting to
// write to it. The path is opened with O_PATH, which opens nothing of what
// it names, so no pipe notices; that open file is checked; and only a
// regular file is then opened for reading, through /proc/self/fd, which
// leads to that same file whatever the path names by then. ok is false
// where /proc isn't there to do so.
func openPinned(path string) (f *os.File, err error, ok bool) {
	fd, err := openRetry(path, unix.O_PATH|unix.O_CLOEXEC)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}, true
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}, true
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, &fs.PathError{Op: "open", Path: path, Err: errNotRegular}, true
	}
	pinned := "/proc/self/fd/" + strconv.Itoa(fd)
	file, err := openRetry(pinned, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK)
	if err == unix.EWOULDBLOCK {
		// a lease (see openNoWait): this one waits until it is given up
		file, err = openRetry(pinned, unix.O_RDONLY|unix.O_CLOEXEC)
	}
	if errors.Is(err, unix.ENOENT) {
		return nil, nil, false // no /proc: the fd is there, its name isn't
	}
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}, true
	}
	return os.NewFile(uintptr(file), path), nil, true
}

// openRetry is open(2), again after a signal cut it short, as os.OpenFile
// does: Go's own signals reach an open that waits for a lease.
func openRetry(path string, flags int) (int, error) {
	for {
		fd, err := unix.Open(path, flags, 0)
		if err != unix.EINTR {
			return fd, err
		}
	}
}

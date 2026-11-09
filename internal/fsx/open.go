package fsx

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

var errNotRegular = errors.New("not a regular file")

// Open opens a file to read what's in it: a regular file, also through a
// symlink, and nothing else. A named pipe blocks until someone writes to
// it and a device may never end, so a file called README.md or go.mod
// that is one of them would hang a listing. The open doesn't wait, and
// what was opened is checked rather than the path, which may have been
// swapped since it was listed.
func Open(path string) (*os.File, error) {
	f, err := openNoWait(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = &fs.PathError{Op: "open", Path: path, Err: errNotRegular}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Regular reports whether path, followed through links, is a regular file.
func Regular(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// ReadFile reads a regular file (see Open), at most limit bytes of it when
// limit is positive.
func ReadFile(path string, limit int64) ([]byte, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if limit > 0 {
		return io.ReadAll(io.LimitReader(f, limit))
	}
	return io.ReadAll(f)
}

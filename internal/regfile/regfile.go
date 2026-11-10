// Package regfile reads regular files, and nothing else.
//
// glint reads files it didn't choose: the README, a manifest, the largest
// source file, whatever a folder holds. Any of them can be a named pipe,
// which blocks whoever opens it until something writes to it, or a device
// that never ends, so a file called go.mod that is one of them would hang
// a listing. Every such read goes through this package.
package regfile

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

var errNotRegular = errors.New("not a regular file")

// Open opens a regular file, also through a symlink, to read it. What the
// path names is checked before it is opened, so that a pipe is left alone
// (opening one for reading wakes the process waiting to write to it), and
// again on the open file, because the path may have been swapped in
// between. The open doesn't wait in either case.
func Open(path string) (*os.File, error) {
	f, _, err := open(path)
	return f, err
}

func open(path string) (*os.File, int64, error) {
	if st, err := os.Stat(path); err != nil {
		return nil, 0, err
	} else if !st.Mode().IsRegular() {
		return nil, 0, &fs.PathError{Op: "open", Path: path, Err: errNotRegular}
	}
	f, err := openNoWait(path)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = &fs.PathError{Op: "open", Path: path, Err: errNotRegular}
	}
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

// Is reports whether path, followed through links, is a regular file.
func Is(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// Read reads a regular file (see Open), at most limit bytes of it when
// limit is positive.
func Read(path string, limit int64) ([]byte, error) {
	f, size, err := open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if limit > 0 {
		r, size = io.LimitReader(f, limit), min(size, limit)
	}
	// as os.ReadFile: room for the size the file reports and a little
	// more, for the ones that report none (/proc)
	data := make([]byte, 0, min(size, 1<<30)+512)
	for {
		n, err := r.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return data, err
		}
		if len(data) == cap(data) {
			data = append(data, 0)[:len(data)]
		}
	}
}

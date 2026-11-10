//go:build !unix

package regfile

import "os"

// openNoWait opens a path for reading. Without O_NONBLOCK, Open's check of
// what it opened still keeps devices out.
func openNoWait(path string) (*os.File, error) { return os.Open(path) }

// OpenDir opens a folder to list it.
func OpenDir(path string) (*os.File, error) { return os.Open(path) }

// createNoWait opens path to replace its contents, creating it.
func createNoWait(path string, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
}

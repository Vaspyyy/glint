//go:build !unix

package regfile

import "os"

// openNoWait opens a path for reading. Without O_NONBLOCK, Open's check of
// what it opened still keeps devices out.
func openNoWait(path string) (*os.File, error) { return os.Open(path) }

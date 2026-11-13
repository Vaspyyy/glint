//go:build unix && !linux

package regfile

import "os"

// openPinned is for Linux only, which can open a file it has looked at
// (see pin_linux.go). Elsewhere openNoWait looks and opens in two steps.
func openPinned(string) (*os.File, error, bool) { return nil, nil, false }

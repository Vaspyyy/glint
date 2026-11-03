//go:build unix

package agent

import (
	"io/fs"
	"syscall"
)

// onDisk is the space a file really occupies (allocated blocks, like du),
// plus an identity for hardlinks so they are counted once.
func onDisk(info fs.FileInfo) (bytes int64, key [2]uint64, linked bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size(), key, false
	}
	return int64(st.Blocks) * 512, [2]uint64{uint64(st.Dev), uint64(st.Ino)}, st.Nlink > 1
}

//go:build !unix

package agent

import "io/fs"

func onDisk(info fs.FileInfo) (bytes int64, key [2]uint64, linked bool) {
	return info.Size(), key, false
}

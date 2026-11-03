//go:build !unix

package fsx

import "io/fs"

func fillSys(e *Entry, info fs.FileInfo, owners bool) {}

func OnDisk(info fs.FileInfo) (bytes int64, key [2]uint64, linked bool) {
	return info.Size(), key, false
}

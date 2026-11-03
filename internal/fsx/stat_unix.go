//go:build unix

package fsx

import (
	"io/fs"
	"os/user"
	"strconv"
	"sync"
	"syscall"
)

var (
	idMu   sync.Mutex
	users  = map[uint32]string{}
	groups = map[uint32]string{}
)

func fillSys(e *Entry, info fs.FileInfo, owners bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	e.Sys.UID = st.Uid
	e.Sys.GID = st.Gid
	e.Sys.Nlink = uint64(st.Nlink)
	e.Sys.Inode = uint64(st.Ino)
	e.Sys.Blocks = int64(st.Blocks)
	if owners {
		e.Sys.Owner = lookupID(users, st.Uid, func(s string) (string, error) {
			u, err := user.LookupId(s)
			if err != nil {
				return "", err
			}
			return u.Username, nil
		})
		e.Sys.Group = lookupID(groups, st.Gid, func(s string) (string, error) {
			g, err := user.LookupGroupId(s)
			if err != nil {
				return "", err
			}
			return g.Name, nil
		})
	}
}

func lookupID(cache map[uint32]string, id uint32, lookup func(string) (string, error)) string {
	idMu.Lock()
	defer idMu.Unlock()
	if n, ok := cache[id]; ok {
		return n
	}
	n := strconv.FormatUint(uint64(id), 10)
	if name, err := lookup(n); err == nil {
		n = name
	}
	cache[id] = n
	return n
}

// OnDisk is the space a file really occupies (allocated blocks, like du),
// plus an identity so hardlinked files can be counted once.
func OnDisk(info fs.FileInfo) (bytes int64, key [2]uint64, linked bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size(), key, false
	}
	return int64(st.Blocks) * 512, [2]uint64{uint64(st.Dev), uint64(st.Ino)}, st.Nlink > 1
}

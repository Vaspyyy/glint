// Package fsx loads directory entries with everything the renderers need.
package fsx

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/glint/internal/regfile"
	"github.com/Vaspyyy/glint/internal/theme"
)

type SysInfo struct {
	Owner, Group string
	UID, GID     uint32
	Nlink        uint64
	Inode        uint64
	Blocks       int64
}

type Entry struct {
	Name    string
	Path    string // path as it should be opened (relative to cwd if the user gave one)
	Abs     string
	Mode    fs.FileMode // from lstat
	Size    int64
	ModTime time.Time

	IsDir  bool // a directory, or a symlink pointing at one
	IsLink bool
	Target string
	Broken bool
	Exec   bool

	Sys SysInfo
	Git git.Status

	DirSize    int64
	HasDirSize bool

	DiskSize int64 // like du: allocated blocks, hardlinks counted once
	Regen    int64 // of which regenerable build output / caches
	HasDisk  bool

	Peek   *Peek
	Commit *git.Commit

	Children []*Entry
	ChildErr error
}

// Peek is a short, human description of what's inside an entry.
type Peek struct {
	Glyph string
	Text  string
	Color theme.Slot
}

type Options struct {
	Hidden    int // 0: skip dotfiles, 1: show them, 2: also show . and ..
	Owners    bool
	OnlyDirs  bool
	OnlyFiles bool
	Ignore    []string
}

func (o Options) ignored(name string) bool {
	for _, pat := range o.Ignore {
		if ok, _ := filepath.Match(pat, name); ok {
			return true
		}
	}
	return false
}

func newEntry(path, abs, name string, info fs.FileInfo, owners bool) *Entry {
	m := info.Mode()
	e := &Entry{
		Name: name, Path: path, Abs: abs,
		Mode: m, Size: info.Size(), ModTime: info.ModTime(),
		IsDir: info.IsDir(),
		Exec:  m.IsRegular() && m.Perm()&0o111 != 0,
	}
	if m&fs.ModeSymlink != 0 {
		e.IsLink = true
		e.Target, _ = os.Readlink(path)
		if st, err := os.Stat(path); err != nil {
			e.Broken = true
		} else {
			e.IsDir = st.IsDir()
		}
	}
	fillSys(e, info, owners)
	return e
}

// Stat builds an entry for a path given on the command line.
func Stat(path string, opt Options) (*Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return newEntry(path, abs, path, info, opt.Owners), nil
}

func ReadDir(dir string, opt Options) ([]*Entry, error) {
	f, err := regfile.OpenDir(dir)
	if err != nil {
		return nil, err
	}
	des, err := f.ReadDir(-1) // unlike os.ReadDir this doesn't sort; we sort later anyway
	f.Close()
	if err != nil && len(des) == 0 {
		return nil, err
	}
	abs, aerr := filepath.Abs(dir)
	if aerr != nil {
		abs = dir
	}

	kept := des[:0]
	for _, de := range des {
		name := de.Name()
		if opt.Hidden == 0 && strings.HasPrefix(name, ".") {
			continue
		}
		if opt.ignored(name) {
			continue
		}
		kept = append(kept, de)
	}

	out := make([]*Entry, len(kept))
	Parallel(len(kept), func(i int) {
		de := kept[i]
		info, err := de.Info()
		if err != nil {
			return // vanished between readdir and lstat
		}
		name := de.Name()
		out[i] = newEntry(filepath.Join(dir, name), filepath.Join(abs, name), name, info, opt.Owners)
	})

	res := out[:0]
	for _, e := range out {
		if e == nil || (opt.OnlyDirs && !e.IsDir) || (opt.OnlyFiles && e.IsDir) {
			continue
		}
		res = append(res, e)
	}

	if opt.Hidden >= 2 && !opt.OnlyFiles {
		var dots []*Entry
		for _, n := range []string{".", ".."} {
			p := filepath.Join(dir, n)
			if info, err := os.Lstat(p); err == nil {
				dots = append(dots, newEntry(p, filepath.Join(abs, n), n, info, opt.Owners))
			}
		}
		res = append(dots, res...)
	}
	return res, nil
}

// Parallel runs fn(0..n-1) on a small worker pool; tiny inputs stay sequential.
func Parallel(n int, fn func(int)) { ParallelMin(n, 48, fn) }

func ParallelMin(n, threshold int, fn func(int)) {
	if n < threshold {
		for i := range n {
			fn(i)
		}
		return
	}
	workers := min(runtime.GOMAXPROCS(0)*2, n)
	var next atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				fn(i)
			}
		}()
	}
	wg.Wait()
}

// BuildTree fills Children recursively. prepare runs on every level so the
// caller can attach git status, filter and sort.
func BuildTree(root *Entry, opt Options, maxDepth int, prepare func([]*Entry) []*Entry) {
	if opt.Hidden > 1 {
		opt.Hidden = 1
	}
	buildTree(root, opt, 1, maxDepth, prepare)
}

func buildTree(e *Entry, opt Options, depth, maxDepth int, prepare func([]*Entry) []*Entry) {
	if maxDepth > 0 && depth > maxDepth {
		return
	}
	kids, err := ReadDir(e.Path, opt)
	if err != nil {
		e.ChildErr = err
		return
	}
	if prepare != nil {
		kids = prepare(kids)
	}
	e.Children = kids
	for _, k := range kids {
		if k.IsDir && !k.IsLink { // never follow symlinked dirs: no cycles
			buildTree(k, opt, depth+1, maxDepth, prepare)
		}
	}
}

// ComputeDirSizes sets DirSize (apparent size of all contained files).
func ComputeDirSizes(es []*Entry) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0)*2)
	for _, e := range es {
		if !e.IsDir || e.IsLink || e.HasDirSize || e.Name == ".." {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			e.DirSize = dirSize(e.Path)
			e.HasDirSize = true
		}()
	}
	wg.Wait()
}

func dirSize(root string) int64 {
	var total int64
	filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// Prune drops directories whose subtree ended up empty after filtering
// (used by --since and --changed in tree mode).
func Prune(es []*Entry, keep func(*Entry) bool) []*Entry {
	out := es[:0]
	for _, e := range es {
		if e.IsDir && e.Children != nil {
			e.Children = Prune(e.Children, keep)
			if len(e.Children) == 0 && !keep(e) {
				continue
			}
		} else if !keep(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

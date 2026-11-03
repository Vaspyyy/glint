package fsx

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Regen describes a folder of regenerable build output or caches.
type Regen struct {
	Path string // relative to the listed directory
	Size int64
	Hint string // how it comes back, e.g. "cargo clean"
}

var regenerable = map[string]string{
	"target": "cargo clean", "cargo-target": "cargo clean",
	"node_modules": "npm install restores it", ".next": "next build recreates it", ".nuxt": "nuxt build recreates it",
	".godot": "Godot reimports it", ".import": "Godot reimports it",
	"__pycache__": "safe to delete", ".mypy_cache": "safe to delete", ".pytest_cache": "safe to delete",
	".ruff_cache": "safe to delete", ".tox": "tox recreates it", ".venv": "pip install restores it", "venv": "pip install restores it",
	".gradle": "gradle recreates it", ".kotlin": "gradle recreates it", "build": "a rebuild recreates it",
	"dist": "a rebuild recreates it", ".cache": "safe to delete", ".parcel-cache": "safe to delete", ".turbo": "safe to delete",
}

// RegenHint reports whether a folder name is regenerable and how.
func RegenHint(name string) (string, bool) {
	h, ok := regenerable[name]
	return h, ok
}

// DiskUsage measures every entry like du (allocated blocks, hardlinks counted
// once) and finds the regenerable folders inside them. Sets DiskSize/Regen.
func DiskUsage(es []*Entry) []Regen {
	var seen sync.Map
	var mu sync.Mutex
	var regens []Regen
	count := func(info fs.FileInfo) int64 {
		b, key, linked := OnDisk(info)
		if linked {
			if _, dup := seen.LoadOrStore(key, true); dup {
				return 0
			}
		}
		return b
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0)*2)
	for _, e := range es {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		info, err := os.Lstat(e.Path)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			e.DiskSize, e.HasDisk = count(info), true
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var total, regen atomic.Int64
			local := map[string]int64{} // regenerable root → size
			filepath.WalkDir(e.Path, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return nil
				}
				b := count(info)
				total.Add(b)
				rel, _ := filepath.Rel(filepath.Dir(e.Path), p)
				parts := strings.Split(filepath.ToSlash(rel), "/")
				for i, part := range parts[:len(parts)-1] {
					if _, ok := regenerable[part]; ok {
						regen.Add(b)
						local[strings.Join(parts[:i+1], "/")] += b
						break
					}
				}
				return nil
			})
			e.DiskSize, e.Regen, e.HasDisk = total.Load(), regen.Load(), true
			mu.Lock()
			for path, size := range local {
				base := filepath.Base(path)
				hint := regenerable[base]
				if base == "target" {
					if _, err := os.Stat(filepath.Join(filepath.Dir(e.Path), filepath.Dir(path), "Cargo.toml")); err != nil {
						hint = "build output"
					}
				}
				regens = append(regens, Regen{Path: path, Size: size, Hint: hint})
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Slice(regens, func(i, j int) bool { return regens[i].Size > regens[j].Size })
	return regens
}

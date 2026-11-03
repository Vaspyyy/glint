package agent

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type usage struct {
	name  string
	dir   bool
	size  int64
	regen int64 // of which regenerable build output / caches
	files int
	kids  map[string]int64 // direct children of a top-level dir
}

// folders whose contents a build or package manager can recreate
var regenerable = map[string]bool{"target": true, "node_modules": true, ".godot": true, "build": true, "dist": true,
	"__pycache__": true, ".gradle": true, ".next": true, ".nuxt": true, ".cache": true, "cargo-target": true,
	".venv": true, "venv": true, ".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true, ".tox": true,
	".parcel-cache": true, ".turbo": true, "out": false, ".kotlin": true, "obj": true, ".import": true}

func isRegen(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if regenerable[part] {
			return true
		}
	}
	return false
}

// DiskUsage prints where the space goes under each path: every top-level
// entry with its share, and for the biggest ones their biggest children.
// Unlike the tree view this counts everything, ignored files included,
// because that's what's actually on disk.
func DiskUsage(w io.Writer, paths []string, opt Options) int {
	status := 0
	for i, p := range paths {
		if i > 0 {
			fmt.Fprintln(w)
		}
		if err := diskUsage(w, p, opt); err != nil {
			fmt.Fprintf(w, "error: %s: %s\n", p, errMsg(err))
			status = 2
		}
	}
	return status
}

func diskUsage(w io.Writer, dir string, opt Options) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if st, err := os.Lstat(abs); err == nil && !st.IsDir() {
		b, _, _ := onDisk(st)
		fmt.Fprintf(w, "%s — %s on disk (%s apparent)\n", abs, human(b), human(st.Size()))
		return nil
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		return err
	}
	var seen sync.Map // hardlinked inodes already counted
	list := make([]*usage, 0, len(ents))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, e := range ents {
		if ignoredByGlob([]string{e.Name()}, opt.Ignore) {
			continue
		}
		u := &usage{name: e.Name(), dir: e.IsDir(), kids: map[string]int64{}}
		list = append(list, u)
		if !e.IsDir() {
			if info, err := e.Info(); err == nil {
				b, key, linked := onDisk(info)
				if _, dup := seen.LoadOrStore(key, true); !linked || !dup {
					u.size = b
				}
				u.files = 1
			}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			base := filepath.Join(abs, u.name)
			filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return nil
				}
				b, key, linked := onDisk(info)
				if linked {
					if _, dup := seen.LoadOrStore(key, true); dup {
						return nil // second name for the same data
					}
				}
				u.size += b
				u.files++
				rel, _ := filepath.Rel(base, p)
				if isRegen(filepath.ToSlash(rel)) || regenerable[u.name] {
					u.regen += b
				}
				first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
				if first != filepath.ToSlash(rel) {
					first += "/"
				}
				u.kids[first] += b
				return nil
			})
		}()
	}
	wg.Wait()

	sort.Slice(list, func(i, j int) bool { return list[i].size > list[j].size })
	var total int64
	files := 0
	for _, u := range list {
		total += u.size
		files += u.files
	}
	pct := func(part, whole int64) string {
		if whole == 0 {
			return "0%"
		}
		v := float64(part) / float64(whole) * 100
		if v < 1 {
			return "<1%"
		}
		return fmt.Sprintf("%.0f%%", v)
	}
	var regen int64
	for _, u := range list {
		regen += u.regen
	}
	fmt.Fprintf(w, "%s — %s on disk in %s %s (like du: allocated blocks, hardlinks counted once, ignored files included)\n", abs, human(total), count(files), plural(files, "file"))
	if regen > 0 {
		fmt.Fprintf(w, "regenerable build output and caches (target/, node_modules/, .godot/, build/, dist/, __pycache__/, ...): %s = %s of the total\n", human(regen), pct(regen, total))
	}
	fmt.Fprintln(w)
	shownN := min(len(list), 25)
	for i, u := range list[:shownN] {
		name := quote(u.name)
		if u.dir {
			name += "/"
		}
		line := fmt.Sprintf("%s %s %s", human(u.size), pct(u.size, total), name)
		if u.dir {
			line += fmt.Sprintf(" (%s %s", count(u.files), plural(u.files, "file"))
			if u.regen > 0 && u.regen*10 >= u.size {
				line += fmt.Sprintf("; %s regenerable", human(u.regen))
			}
			line += ")"
		}
		if u.dir && i < 10 && len(u.kids) > 1 && u.size > total/50 {
			type kv struct {
				k string
				v int64
			}
			var ks []kv
			for k, v := range u.kids {
				ks = append(ks, kv{k, v})
			}
			sort.Slice(ks, func(a, b int) bool { return ks[a].v > ks[b].v })
			var parts []string
			for j, k := range ks {
				if j == 4 || k.v < u.size/50 {
					break
				}
				label := fmt.Sprintf("%s %s", quote(k.k), human(k.v))
				if regenerable[strings.TrimSuffix(k.k, "/")] {
					label += " [regenerable]"
				}
				parts = append(parts, label)
			}
			if len(parts) > 0 {
				line += " → " + strings.Join(parts, ", ")
			}
		}
		fmt.Fprintln(w, line)
	}
	if rest := list[shownN:]; len(rest) > 0 {
		var sz int64
		for _, u := range rest {
			sz += u.size
		}
		fmt.Fprintf(w, "… +%d smaller entries, %s total\n", len(rest), human(sz))
	}
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

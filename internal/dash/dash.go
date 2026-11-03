// Package dash scans a folder of projects: one row per project with its
// stack, git state, recent activity and source size.
package dash

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/glint/internal/project"
)

const Weeks = 12

type Row struct {
	Name, Abs string
	Info      *project.Info
	Repo      *git.Repo
	Changes   int
	Last      *git.Commit
	Weekly    []int
	Files     int
	Size      int64 // what git sees (or, outside git, without build output and caches)
	Newest    time.Time
	Empty     bool
}

// Active is the most recent sign of life: last commit or newest file.
func (r *Row) Active() time.Time {
	t := r.Newest
	if r.Last != nil && r.Last.Time.After(t) {
		t = r.Last.Time
	}
	return t
}

var skip = map[string]bool{"node_modules": true, "target": true, "__pycache__": true, ".venv": true, "venv": true,
	"dist": true, "build": true, ".next": true, ".gradle": true, ".cache": true, ".godot": true, ".git": true}

// Scan builds a row for every directory directly inside dir.
func Scan(dir string, hidden bool) ([]*Row, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var rows []*Row
	for _, e := range ents {
		if !e.IsDir() || (!hidden && strings.HasPrefix(e.Name(), ".")) {
			continue
		}
		rows = append(rows, &Row{Name: e.Name(), Abs: filepath.Join(dir, e.Name())})
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 12)
	for _, r := range rows {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fill(r)
		}()
	}
	wg.Wait()
	return rows, nil
}

func fill(r *Row) {
	names, _ := project.Names(r.Abs)
	r.Empty = len(names) == 0
	r.Info = project.Detect(r.Abs, names)
	if r.Info != nil && r.Info.Stack == "git repo" {
		r.Info = nil // the state column already says it's a repo
	}
	if _, err := os.Lstat(filepath.Join(r.Abs, ".git")); err == nil {
		if repo, err := git.Open(r.Abs); err == nil && repo != nil && repo.Root == r.Abs {
			r.Repo = repo
			r.Changes = len(repo.Changes(r.Abs))
			r.Last = repo.LastCommit()
			r.Weekly = repo.Weekly(Weeks)
			sizeOf(r, repo.TrackedFiles(r.Abs))
			return
		}
	}
	var files []string
	deadline := time.Now().Add(time.Second)
	filepath.WalkDir(r.Abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != r.Abs && (skip[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) > 50_000 || time.Now().After(deadline) {
			return filepath.SkipAll
		}
		files = append(files, p)
		return nil
	})
	sizeOf(r, files)
}

func sizeOf(r *Row, files []string) {
	sizes := make([]int64, len(files))
	mtimes := make([]time.Time, len(files))
	fsx.Parallel(len(files), func(i int) {
		if info, err := os.Lstat(files[i]); err == nil && !info.IsDir() {
			sizes[i], mtimes[i] = info.Size(), info.ModTime()
		}
	})
	for i := range files {
		if !mtimes[i].IsZero() {
			r.Files++
			r.Size += sizes[i]
			if mtimes[i].After(r.Newest) {
				r.Newest = mtimes[i]
			}
		}
	}
}

// Sort orders rows: "time" (most recently active first, the default), "size" or "name".
func Sort(rows []*Row, key string, reverse bool) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		var less bool
		switch key {
		case "size":
			less = a.Size > b.Size
		case "name":
			less = fsx.NaturalCompare(a.Name, b.Name) < 0
		default:
			less = a.Active().After(b.Active())
		}
		if reverse {
			return !less
		}
		return less
	})
}

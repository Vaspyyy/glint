// Package git reads repository status with a single `git status` call.
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Status is a porcelain v1 XY pair: X = index, Y = worktree.
type Status struct{ X, Y byte }

func blank(c byte) bool { return c == 0 || c == ' ' }

func (s Status) Clean() bool     { return blank(s.X) && blank(s.Y) }
func (s Status) Ignored() bool   { return s.X == '!' }
func (s Status) Untracked() bool { return s.X == '?' }

func (s Status) String() string {
	if s.Clean() {
		return ""
	}
	c := func(b byte) byte {
		if blank(b) {
			return '.'
		}
		return b
	}
	return string([]byte{c(s.X), c(s.Y)})
}

var rank = [256]int{'!': 1, '?': 2, 'T': 3, 'M': 4, 'C': 5, 'R': 6, 'A': 7, 'D': 8, 'U': 9}

func pick(a, b byte) byte {
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func merge(a, b Status) Status { return Status{pick(a.X, b.X), pick(a.Y, b.Y)} }

type Repo struct {
	Root          string
	Branch        string
	Upstream      string
	NoCommits     bool // fresh repo: branch has no commits yet
	Ahead, Behind int

	files map[string]Status // exact paths from git, relative to Root
	dirs  map[string]Status // statuses aggregated onto parent directories
}

// FindRoot walks up from abs looking for a .git entry, without spawning git.
func FindRoot(abs string) string {
	for d := abs; ; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// Open returns nil (and no error) when dir is not inside a git work tree.
func Open(dir string) (*Repo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root := FindRoot(abs)
	if root == "" {
		return nil, nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, nil
	}
	cmd := exec.Command("git", "-C", root, "status", "--porcelain=v1", "-z", "--branch",
		"--ignored=matching", "--untracked-files=normal", "--", abs)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	r := &Repo{Root: root, files: map[string]Status{}, dirs: map[string]Status{}}
	r.parse(out)
	return r, nil
}

func (r *Repo) parse(out []byte) {
	recs := bytes.Split(out, []byte{0})
	for i := 0; i < len(recs); i++ {
		rec := string(recs[i])
		if b, ok := strings.CutPrefix(rec, "## "); ok {
			r.parseBranch(b)
			continue
		}
		if len(rec) < 4 {
			continue
		}
		st := Status{rec[0], rec[1]}
		p := strings.TrimSuffix(rec[3:], "/")
		if st.X == 'R' || st.X == 'C' || st.Y == 'R' || st.Y == 'C' {
			i++ // skip the original path of a rename/copy
		}
		r.files[p] = merge(r.files[p], st)
		if st.Ignored() {
			continue
		}
		for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
			r.dirs[d] = merge(r.dirs[d], st)
		}
	}
}

func (r *Repo) parseBranch(s string) {
	for _, pre := range []string{"No commits yet on ", "Initial commit on "} {
		if rest, ok := strings.CutPrefix(s, pre); ok {
			r.Branch, r.NoCommits = rest, true
			return
		}
	}
	head, track, _ := strings.Cut(s, " [")
	r.Branch, r.Upstream, _ = strings.Cut(head, "...")
	if r.Branch == "HEAD (no branch)" {
		r.Branch = "detached"
	}
	for _, part := range strings.Split(strings.TrimSuffix(track, "]"), ", ") {
		fmt.Sscanf(part, "ahead %d", &r.Ahead)
		fmt.Sscanf(part, "behind %d", &r.Behind)
	}
}

// Status of an absolute path. Directories aggregate their contents, and paths
// inside an untracked or ignored directory inherit that state.
func (r *Repo) Status(abs string) Status {
	rel, err := filepath.Rel(r.Root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return Status{}
	}
	rel = filepath.ToSlash(rel)
	s := r.files[rel]
	if d, ok := r.dirs[rel]; ok {
		s = merge(s, d)
	}
	if s.Clean() {
		for p := path.Dir(rel); p != "."; p = path.Dir(p) {
			if a, ok := r.files[p]; ok && (a.Ignored() || a.Untracked()) {
				return a
			}
		}
	}
	return s
}

func (r *Repo) Counts() (staged, modified, untracked int) {
	for _, s := range r.files {
		switch {
		case s.Ignored():
		case s.Untracked():
			untracked++
		default:
			if !blank(s.X) {
				staged++
			}
			if !blank(s.Y) {
				modified++
			}
		}
	}
	return
}

// Change is one non-ignored status entry, relative to the directory asked about.
type Change struct {
	Path   string
	Status Status
}

// relTo maps a repo-relative path to one relative to dirAbs ("" if outside).
func (r *Repo) relTo(dirAbs, p string) string {
	full := filepath.Join(r.Root, filepath.FromSlash(p))
	rel, err := filepath.Rel(dirAbs, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return filepath.ToSlash(rel)
}

// Changes lists uncommitted changes under dirAbs, sorted by path.
func (r *Repo) Changes(dirAbs string) []Change {
	var out []Change
	for p, s := range r.files {
		if s.Ignored() || s.Clean() {
			continue
		}
		if rel := r.relTo(dirAbs, p); rel != "" && rel != "." {
			out = append(out, Change{rel, s})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Ignored lists ignored entries under dirAbs (directories end in "/").
func (r *Repo) Ignored(dirAbs string) []string {
	var out []string
	for p, s := range r.files {
		if !s.Ignored() {
			continue
		}
		if rel := r.relTo(dirAbs, p); rel != "" && rel != "." {
			if st, err := os.Lstat(filepath.Join(dirAbs, rel)); err == nil && st.IsDir() {
				rel += "/"
			}
			out = append(out, rel)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := strings.Count(out[i], "/"), strings.Count(out[j], "/")
		if di != dj {
			return di < dj
		}
		return out[i] < out[j]
	})
	return out
}

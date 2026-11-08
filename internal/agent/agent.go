// Package agent renders glint for AI coding agents: no color, no padding,
// no icons, just the facts an agent needs to orient itself, packed into a
// predictable line budget.
package agent

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/glint/internal/img"
	"github.com/Vaspyyy/glint/internal/project"
	"github.com/Vaspyyy/prism/repo"
)

type Options struct {
	Budget   int           // approximate max lines for the tree
	MaxDepth int           // 0 = unlimited
	PerDir   int           // max children listed per directory
	All      bool          // don't skip build/cache directories outside git
	Since    time.Duration // only files modified within this window
	SinceRaw string        // the user's spelling of Since, for the header
	Changed  bool          // only files with uncommitted changes
	Commits  bool          // last commit for top-level entries
	Outline  bool          // symbol list under each shown source file
	Ignore   []string      // name globs to drop
	NoGit    bool
	Long     bool // mode and age on every line

	sameAge   string // set when most top-level entries share an age: said once in the header
	workspace bool   // listing a folder of projects that isn't itself a repo
}

func (o *Options) defaults() {
	if o.Budget <= 0 {
		o.Budget = 200
	}
	if o.PerDir <= 0 {
		o.PerDir = 40
	}
}

type node struct {
	name     string
	rel      string // slash path relative to the listing root
	abs      string
	dir      bool
	kids     []*node
	byName   map[string]*node
	files    int
	size     int64
	mtime    time.Time
	mode     fs.FileMode
	target   string
	git      git.Status
	exts     map[string]int
	sub      bool        // git submodule / nested repo boundary
	skipped  bool        // dir we deliberately didn't scan
	empty    bool        // dir with no entries at all
	hollow   string      // dir with entries but no listed files: why ("only gitignored files", ...)
	partial  bool        // scan hit its time/size limit inside this dir
	glance   bool        // collapsed, but its child names get one line
	series   []*node     // members, for a node standing in for a run of similar entries
	gitlink  bool        // a directory entry git lists as one item (submodule / nested repo)
	repo     *nestedRepo // set on the dir of a repo found inside a non-git scan
	vendored bool
	recent   bool   // opened because it is one of the most recently active projects
	changedN int    // listed files with uncommitted changes below this dir
	newN     int    // ...of which untracked
	like     string // for a dir series: the sibling it resembles

	expanded bool
	lines    int
	binary   bool
	note     string // e.g. image dimensions
	project  *project.Info
	commit   *git.Commit
	symbols  []Symbol
	refs     []string // each symbol's name as a ref prints it
}

// directories that are almost never worth an agent's tokens outside git
var junk = map[string]bool{"node_modules": true, "target": true, "__pycache__": true, ".venv": true, "venv": true,
	"dist": true, "build": true, ".next": true, ".nuxt": true, ".gradle": true, ".cache": true, ".tox": true,
	".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true, ".godot": true, ".import": true, "obj": true,
	".idea": true, ".git": true, ".svn": true, ".hg": true, ".DS_Store": true, ".terraform": true, "coverage": true}

var keepHidden = map[string]bool{".github": true, ".gitlab": true, ".vscode": true, ".devcontainer": true,
	".circleci": true, ".husky": true, ".cargo": true, ".config": true, ".opencode": true, ".claude": true, ".agents": true}

const (
	maxScan     = 300_000 // git-listed files (cheap: no walk)
	maxWalk     = 100_000 // files walked outside git
	rootPer     = 100     // top-level entries that always get a full line
	scanTimeout = 1500 * time.Millisecond
)

// Run prints an agent view for every path and returns an exit status.
func Run(w io.Writer, paths []string, opt Options) int {
	opt.defaults()
	status := 0
	var here func() *repo.Info // the working directory's repository, looked up for the first file
	for i, p := range paths {
		if i > 0 {
			fmt.Fprintln(w)
		}
		st, err := os.Stat(p)
		if err != nil {
			fmt.Fprintf(w, "error: %s: %s\n", p, errMsg(err))
			status = 2
			continue
		}
		if st.IsDir() {
			listDir(w, p, opt)
		} else {
			if here == nil && !opt.NoGit {
				here = workRepo()
			}
			describeFile(w, p, st, opt, here)
		}
	}
	return status
}

func errMsg(err error) string {
	var pe *fs.PathError
	if e, ok := err.(*fs.PathError); ok {
		pe = e
		return pe.Err.Error()
	}
	return err.Error()
}

type item struct {
	abs    string
	info   fs.FileInfo
	git    git.Status
	hasGit bool // listed by a nested repo, so git is the authority on it
}

// nestedRepo is a git repo found inside a non-git scan.
type nestedRepo struct {
	repo    *git.Repo
	changes int
	last    *git.Commit
}

type scan struct {
	files       []string // absolute paths (git listing, before stat)
	items       []item   // stat'ed files
	skipped     []string // relative dirs skipped as junk/hidden
	partial     bool
	partialDirs map[string]bool        // top-level dirs whose walk was cut short
	nested      map[string]bool        // relative dirs that are their own git repos
	repos       map[string]*nestedRepo // nested repos found while walking, by relative path
	repo        *git.Repo
	ignored     []string
	changes     []git.Change
	rootAbs     string
	note        string
	statusOf    func(abs string) git.Status
}

func gather(root string, opt Options) *scan {
	abs, _ := filepath.Abs(root)
	s := &scan{rootAbs: abs, nested: map[string]bool{}, partialDirs: map[string]bool{}, repos: map[string]*nestedRepo{},
		statusOf: func(string) git.Status { return git.Status{} }}
	if !opt.NoGit {
		repo, err := git.Open(abs)
		switch {
		case err != nil || repo == nil:
		case repo.Root != abs && repo.Status(abs).Ignored():
			s.note = "this folder is gitignored by the repo at " + repo.Root + "; listing what is on disk"
		default:
			s.repo = repo
			s.statusOf = repo.Status
			s.ignored = repo.Ignored(abs)
			s.changes = repo.Changes(abs)
			s.files = repo.TrackedFiles(abs)
			if len(s.files) > maxScan {
				s.files, s.partial = s.files[:maxScan], true
			}
			return s
		}
	}
	s.walk(abs, opt)
	return s
}

// listRepo lists a nested repo exactly the way `glint -A <repo>` would
// (tracked + untracked, minus ignored), so a project never has two sizes.
func (s *scan) listRepo(dir, rel string, mu *sync.Mutex) ([]item, bool) {
	repo, err := git.Open(dir)
	if err != nil || repo == nil || repo.Root != dir {
		return nil, false
	}
	files := repo.TrackedFiles(dir)
	items := make([]item, len(files))
	fsx.Parallel(len(files), func(i int) {
		if info, err := os.Lstat(files[i]); err == nil {
			items[i] = item{abs: files[i], info: info, git: repo.Status(files[i]), hasGit: true}
		}
	})
	kept := items[:0]
	for _, it := range items {
		if it.info != nil {
			kept = append(kept, it)
		}
	}
	nr := &nestedRepo{repo: repo, changes: len(repo.Changes(dir))}
	if !repo.NoCommits {
		nr.last = repo.LastCommit()
	}
	mu.Lock()
	s.repos[rel] = nr
	s.nested[rel] = true
	mu.Unlock()
	return kept, true
}

// walk scans a non-git tree in parallel, one goroutine per top-level
// directory, keeping the FileInfo from the walk so nothing is stat'ed twice.
func (s *scan) walk(abs string, opt Options) {
	deadline := time.Now().Add(scanTimeout)
	var mu sync.Mutex
	var total atomic.Int64
	skip := func(name string) bool {
		return !opt.All && (junk[name] || (strings.HasPrefix(name, ".") && !keepHidden[name]))
	}
	var walkOne func(dir string) ([]item, []string, map[string]bool, bool)
	walkOne = func(dir string) (items []item, skipped []string, nested map[string]bool, partial bool) {
		nested = map[string]bool{}
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				rel, _ := filepath.Rel(abs, p)
				if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil && !opt.NoGit {
					if its, ok := s.listRepo(p, filepath.ToSlash(rel), &mu); ok {
						items = append(items, its...)
						total.Add(int64(len(its)))
						return filepath.SkipDir
					}
				}
				if p != dir && skip(d.Name()) {
					if d.Name() != ".git" { // a nested .git is reported as "own git repo" instead
						skipped = append(skipped, filepath.ToSlash(rel)+"/")
					}
					return filepath.SkipDir
				}
				if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
					nested[filepath.ToSlash(rel)] = true
				}
				return nil
			}
			if total.Add(1) > maxWalk || time.Now().After(deadline) {
				partial = true
				return filepath.SkipAll
			}
			if info, err := d.Info(); err == nil {
				items = append(items, item{abs: p, info: info})
			}
			return nil
		})
		return
	}

	ents, err := os.ReadDir(abs)
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, e := range ents {
		p := filepath.Join(abs, e.Name())
		// the walkers started for earlier folders append to the same
		// slices, so this loop takes the lock too
		if !e.IsDir() {
			if info, err := e.Info(); err == nil {
				mu.Lock()
				s.items = append(s.items, item{abs: p, info: info})
				mu.Unlock()
			}
			continue
		}
		if skip(e.Name()) {
			if e.Name() != ".git" {
				mu.Lock()
				s.skipped = append(s.skipped, e.Name()+"/")
				mu.Unlock()
			}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			items, skipped, nested, partial := walkOne(p)
			mu.Lock()
			s.items = append(s.items, items...)
			s.skipped = append(s.skipped, skipped...)
			for k := range nested {
				s.nested[k] = true
			}
			s.partial = s.partial || partial
			if partial {
				s.partialDirs[e.Name()] = true
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
}

func newDir(name, rel, abs string) *node {
	return &node{name: name, rel: rel, abs: abs, dir: true, byName: map[string]*node{}, exts: map[string]int{}}
}

func extKey(name string) string {
	lower := strings.ToLower(name)
	for {
		i := strings.LastIndexByte(lower, '.')
		if i <= 0 {
			if len(lower) <= 14 && strings.Trim(lower, "abcdefghijklmnopqrstuvwxyz0123456789_-") == "" {
				return lower // Makefile, LICENSE, ...
			}
			return "(no ext)"
		}
		ext := lower[i+1:]
		if ext != "" && strings.Trim(ext, "0123456789") == "" && strings.LastIndexByte(lower[:i], '.') > 0 {
			lower = lower[:i] // libfoo.so.0 → so, but app.0 stays "0"
			continue
		}
		if len(ext) > 10 || strings.Trim(ext, "abcdefghijklmnopqrstuvwxyz0123456789_+-") != "" {
			return "other"
		}
		return ext
	}
}

func build(s *scan, opt Options) (*node, int) {
	root := newDir(filepath.Base(s.rootAbs), "", s.rootAbs)
	items := s.items
	if s.files != nil {
		items = make([]item, len(s.files))
		fsx.Parallel(len(s.files), func(i int) {
			if info, err := os.Lstat(s.files[i]); err == nil {
				items[i] = item{abs: s.files[i], info: info}
			}
		})
	}
	cutoff := time.Now().Add(-opt.Since)
	total := 0
	for _, it := range items {
		if it.info == nil {
			continue // deleted in the work tree; shows up under "changed"
		}
		rel, err := filepath.Rel(s.rootAbs, it.abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		if ignoredByGlob(parts, opt.Ignore) {
			continue
		}
		if opt.Since > 0 && !it.info.ModTime().After(cutoff) {
			continue
		}
		var gs git.Status
		known := it.hasGit
		switch {
		case it.hasGit:
			gs = it.git
		case s.repo != nil:
			gs, known = s.statusOf(it.abs), true
		}
		// outside any repo there is nothing to call "changed"
		if opt.Changed && (!known || gs.Clean() || gs.Ignored()) {
			continue
		}
		total++
		cur := root
		for i, part := range parts[:len(parts)-1] {
			next, ok := cur.byName[part]
			if !ok {
				r := strings.Join(parts[:i+1], "/")
				next = newDir(part, r, filepath.Join(s.rootAbs, filepath.FromSlash(r)))
				next.sub = s.nested[r]
				cur.byName[part] = next
				cur.kids = append(cur.kids, next)
			}
			cur = next
		}
		name := parts[len(parts)-1]
		leaf := &node{name: name, rel: rel, abs: it.abs, mode: it.info.Mode(), size: it.info.Size(), mtime: it.info.ModTime(), git: gs}
		if it.info.IsDir() { // submodule, or a nested repo seen from the outer repo
			leaf.dir, leaf.sub, leaf.gitlink, leaf.byName, leaf.exts = true, true, true, map[string]*node{}, map[string]int{}
		}
		if it.info.Mode()&fs.ModeSymlink != 0 {
			leaf.target, _ = os.Readlink(it.abs)
		}
		cur.byName[name] = leaf
		cur.kids = append(cur.kids, leaf)
	}
	// directories we skipped while walking still deserve a line
	for _, sk := range s.skipped {
		rel := strings.TrimSuffix(sk, "/")
		parts := strings.Split(rel, "/")
		if ignoredByGlob(parts, opt.Ignore) {
			continue
		}
		cur := root
		for i, part := range parts {
			next, ok := cur.byName[part]
			if !ok {
				r := strings.Join(parts[:i+1], "/")
				next = newDir(part, r, filepath.Join(s.rootAbs, filepath.FromSlash(r)))
				next.sub = s.nested[r]
				cur.byName[part] = next
				cur.kids = append(cur.kids, next)
			}
			cur = next
		}
		cur.skipped = true
	}
	// every real top-level entry gets a line, even if nothing inside it was
	// listed: an agent must never conclude that an existing folder is absent
	if opt.Since == 0 && !opt.Changed {
		ents, _ := os.ReadDir(s.rootAbs)
		for _, e := range ents {
			name := e.Name()
			if _, ok := root.byName[name]; ok || name == ".git" || ignoredByGlob([]string{name}, opt.Ignore) {
				continue
			}
			abs := filepath.Join(s.rootAbs, name)
			if s.repo != nil && s.statusOf(abs).Ignored() {
				continue // listed in the "gitignored" header line
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			var n *node
			if info.IsDir() {
				n = newDir(name, name, abs)
				n.mtime = info.ModTime()
				_, gerr := os.Lstat(filepath.Join(abs, ".git"))
				n.sub = gerr == nil
				inner, _ := os.ReadDir(abs)
				switch {
				case len(inner) == 0:
					n.empty = true
				case s.partialDirs[name]:
					n.partial = true
				case n.sub && len(inner) == 1:
					n.hollow = "only .git"
				case s.repo != nil:
					n.hollow = "only gitignored or empty dirs"
				default:
					n.hollow = "only hidden/skipped content"
				}
			} else {
				n = &node{name: name, rel: name, abs: abs, mode: info.Mode(), size: info.Size(), mtime: info.ModTime()}
				if info.Mode()&fs.ModeSymlink != 0 {
					n.target, _ = os.Readlink(abs)
				}
			}
			root.byName[name] = n
			root.kids = append(root.kids, n)
		}
	}
	for name := range s.partialDirs {
		if n, ok := root.byName[name]; ok {
			n.partial = true
		}
	}
	for rel, nr := range s.repos {
		cur := root
		for _, part := range strings.Split(rel, "/") {
			if cur = cur.byName[part]; cur == nil {
				break
			}
		}
		if cur != nil {
			cur.repo, cur.sub = nr, true
		}
	}
	aggregate(root, s)
	if opt.Since > 0 || opt.Changed {
		root.kids = pruneEmpty(root.kids)
	}
	markVendored(root, false)
	groupSeries(root)
	return root, total
}

// addEmptyDirs lists empty folders inside expanded folders: they hold no
// files, so the file-based tree would otherwise never show them, and an agent
// would conclude they don't exist.
func addEmptyDirs(n *node, opt Options) {
	for _, k := range n.kids {
		if k.dir && k.expanded {
			addEmptyDirs(k, opt)
		}
	}
	if n.rel == "" || !n.expanded {
		return // the top level already lists everything
	}
	ents, err := os.ReadDir(n.abs)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() || n.byName[e.Name()] != nil || ignoredByGlob([]string{e.Name()}, opt.Ignore) {
			continue
		}
		p := filepath.Join(n.abs, e.Name())
		if inner, err := os.ReadDir(p); err == nil && len(inner) == 0 {
			d := newDir(e.Name(), n.rel+"/"+e.Name(), p)
			d.empty = true
			if info, err := e.Info(); err == nil {
				d.mtime = info.ModTime()
			}
			n.byName[d.name] = d
			n.kids = append(n.kids, d)
		}
	}
	sort.SliceStable(n.kids, func(i, j int) bool {
		a, b := n.kids[i], n.kids[j]
		if a.dir != b.dir {
			return a.dir
		}
		return fsx.NaturalCompare(a.name, b.name) < 0
	})
}

// pruneEmpty drops folders that ended up with no matching files.
func pruneEmpty(kids []*node) []*node {
	out := kids[:0]
	for _, k := range kids {
		if k.dir {
			if k.files == 0 {
				continue
			}
			k.kids = pruneEmpty(k.kids)
		}
		out = append(out, k)
	}
	return out
}

var vendorDirs = map[string]bool{"vendor": true, "third_party": true, "thirdparty": true, "third-party": true,
	"3rdparty": true, "external": true, "extern": true, "deps": true}

func markVendored(n *node, inherited bool) {
	n.vendored = inherited || (n.dir && vendorDirs[strings.ToLower(n.name)])
	for _, k := range n.kids {
		markVendored(k, n.vendored)
	}
}

func ignoredByGlob(parts, globs []string) bool {
	for _, g := range globs {
		for _, p := range parts {
			if ok, _ := filepath.Match(g, p); ok {
				return true
			}
		}
	}
	return false
}

func aggregate(n *node, s *scan) {
	if !n.dir {
		return
	}
	for _, k := range n.kids {
		if k.dir {
			aggregate(k, s)
			n.files += k.files
			n.size += k.size
			for e, c := range k.exts {
				n.exts[e] += c
			}
			n.changedN += k.changedN
			n.newN += k.newN
		} else {
			n.files++
			n.size += k.size
			n.exts[extKey(k.name)]++
			if !k.git.Clean() && !k.git.Ignored() {
				n.changedN++
				if k.git.Untracked() {
					n.newN++
				}
			}
		}
		if k.mtime.After(n.mtime) {
			n.mtime = k.mtime
		}
	}
	if s.repo != nil && n.rel != "" {
		n.git = s.statusOf(n.abs)
	}
	sort.SliceStable(n.kids, func(i, j int) bool {
		a, b := n.kids[i], n.kids[j]
		if a.dir != b.dir {
			return a.dir
		}
		return fsx.NaturalCompare(a.name, b.name) < 0
	})
}

// ------------------------------------------------------------ budgeting

var boost = map[string]int{"src": 3, "lib": 3, "app": 3, "cmd": 3, "internal": 3, "pkg": 2, "crates": 2,
	"packages": 2, "core": 2, "server": 2, "client": 2, "api": 2, "scripts": 1, "include": 1, "web": 1}
var penalty = map[string]int{"vendor": 4, "third_party": 4, "thirdparty": 4, "external": 3, "deps": 3, "assets": 2,
	"fixtures": 3, "testdata": 3, "snapshots": 3, "locales": 3, "i18n": 3, "static": 2, "public": 2, "docs": 1,
	"examples": 1, "benchmarks": 1, "bench": 1, "generated": 4, "gen": 2, "migrations": 2}

func priority(n *node) int {
	l := strings.ToLower(n.name)
	return boost[l] - penalty[l]
}

// shown returns the children that will be printed for an expanded dir and
// how many were left out.
func shown(n *node, per int) ([]*node, int) {
	if n.rel == "" {
		per = max(per, rootPer) // the top level is listed in full whenever reasonable
	}
	if len(n.kids) <= per {
		return n.kids, 0
	}
	var dirs, files []*node
	for _, k := range n.kids {
		if k.dir {
			dirs = append(dirs, k)
		} else {
			files = append(files, k)
		}
	}
	keepDirs := min(len(dirs), max(per/2, per-len(files)))
	if len(dirs) > keepDirs {
		ranked := append([]*node{}, dirs...)
		sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].files > ranked[j].files })
		dirs = keepOrder(dirs, ranked[:keepDirs])
	}
	keepFiles := min(len(files), per-len(dirs))
	if len(files) > keepFiles {
		ranked := append([]*node{}, files...)
		sort.SliceStable(ranked, func(i, j int) bool {
			pi, pj := fileRank(ranked[i].name), fileRank(ranked[j].name)
			if pi != pj {
				return pi > pj
			}
			return ranked[i].mtime.After(ranked[j].mtime)
		})
		files = keepOrder(files, ranked[:keepFiles])
	}
	out := append(dirs, files...)
	return out, len(n.kids) - len(out)
}

func keepOrder(all, keep []*node) []*node {
	set := map[*node]bool{}
	for _, k := range keep {
		set[k] = true
	}
	var out []*node
	for _, a := range all {
		if set[a] {
			out = append(out, a)
		}
	}
	return out
}

// fileRank favours files an agent should see first when a dir is truncated.
func fileRank(name string) int {
	l := strings.ToLower(name)
	switch {
	case strings.HasPrefix(l, "readme"), l == "agents.md", l == "claude.md":
		return 5
	case l == "go.mod", l == "cargo.toml", l == "package.json", l == "pyproject.toml", l == "makefile",
		l == "cmakelists.txt", l == "build.gradle", l == "build.gradle.kts", l == "project.godot", l == "dockerfile":
		return 4
	case strings.HasPrefix(l, "main."), strings.HasPrefix(l, "index."), strings.HasPrefix(l, "lib."),
		strings.HasPrefix(l, "mod."), strings.HasPrefix(l, "app."), strings.HasPrefix(l, "__init__."):
		return 3
	case strings.HasSuffix(l, ".lock"), strings.HasSuffix(l, ".sum"), strings.HasSuffix(l, "-lock.json"):
		return -1
	}
	return 0
}

func cost(n *node, per int) int {
	_, rest := shown(n, per)
	c := min(len(n.kids), per)
	if rest > 0 {
		c++
	}
	return c
}

var codeExts = map[string]bool{}
var dataExts = map[string]bool{}
var binExts = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`go rs py pyi js mjs cjs ts mts tsx jsx c h cc cpp cxx hpp hh cs java kt kts rb php lua
		sh bash zsh fish swift zig dart vue svelte astro html css scss sass less gd gdshader glsl wgsl hlsl sql nix ex exs
		hs ml scala clj el vim toml yaml yml jsonc md mdx rst ini cfg conf mk cmake makefile dockerfile justfile
		proto graphql tf hcl gradle properties r jl m mm`) {
		codeExts[e] = true
	}
	// text, but usually generated or recorded rather than written: worth far less per line
	for _, e := range strings.Fields(`txt json jsonl ndjson xml csv tsv log err out trace prof tas snap lock sum map tscn uid`) {
		dataExts[e] = true
	}
	for _, e := range strings.Fields(`png jpg jpeg gif webp bmp ico tif tiff psd xcf svg ogg wav mp3 flac mid midi opus m4a aac
		mp4 mkv webm mov avi zip gz tgz tar xz bz2 zst 7z rar woff woff2 ttf otf eot pdf o a so dll dylib exe bin dat pak
		wasm blend fbx glb gltf obj mtl dds ktx2 exr hdr lockb pyc class jar iso img import ctex res scn tres`) {
		binExts[e] = true
	}
}

// value is how much an agent gains from seeing a directory's contents:
// code and docs are worth a lot, piles of media almost nothing.
func value(n *node) float64 {
	if n.files == 0 {
		return 0
	}
	var code, other float64
	for e, c := range n.exts {
		switch {
		case codeExts[e]:
			code += float64(c)
		case dataExts[e]:
			other += 0.3 * float64(c)
		case !binExts[e]:
			other += 0.25 * float64(c)
		}
	}
	return (code + other) / float64(n.files)
}

func expandable(k *node) bool {
	return k.dir && !k.sub && !k.skipped && !k.empty && k.hollow == "" && k.series == nil && len(k.kids) > 0
}

// expand opens directories in order of value (code first, any depth) until
// the line budget is used up. Media and data folders open last, if at all.
// In a workspace of projects every project first gets a one-line glance.
func expand(root *node, opt Options) int {
	root.expanded = true
	used := cost(root, opt.PerDir)

	// a folder of projects: give every top-level folder a glance before
	// spending the budget on any single one of them
	projects := 0
	for _, k := range root.kids {
		if k.dir && (k.project != nil || k.sub) {
			projects++
		}
	}
	workspace := opt.workspace && projects >= 3
	if workspace {
		for _, k := range root.kids {
			if expandable(k) || (k.sub && len(k.kids) > 0) {
				if used >= opt.Budget {
					break
				}
				k.glance = true
				used++
			}
		}
	}

	type cand struct {
		n         *node
		depth     int
		inherited int
		score     float64
	}
	var queue []cand
	push := func(parent *node, depth int, inherited int) {
		if opt.MaxDepth > 0 && depth >= opt.MaxDepth {
			return
		}
		kids, _ := shown(parent, opt.PerDir)
		for _, k := range kids {
			if !expandable(k) || (k.glance && (k.project != nil || k.sub)) {
				continue
			}
			pr := inherited + priority(k)
			queue = append(queue, cand{k, depth, inherited, value(k)*10 + float64(pr)*2 - float64(depth)*3 - float64(cost(k, opt.PerDir))*0.05})
		}
	}
	// penalties are inherited (everything under vendor/ is vendor), boosts are not
	inherited := map[*node]int{root: 0}
	push(root, 1, 0)
	var closed []cand
	for len(queue) > 0 {
		best := 0
		for i := range queue {
			if queue[i].score > queue[best].score {
				best = i
			}
		}
		c := queue[best]
		queue = append(queue[:best], queue[best+1:]...)
		cc := cost(c.n, opt.PerDir)
		// listing a pile of media or data files tells an agent nothing the
		// summary line (ogg×35 zip×1) doesn't; leave those closed unless tiny
		// in a workspace each project is its own world (glint -A <project>);
		// only small loose folders are worth opening from up here
		tooDeep := workspace && (c.depth > 1 || cc > 8)
		if used+cc > opt.Budget || (value(c.n) < 0.35 && cc > 3) || tooDeep {
			closed = append(closed, c)
			continue
		}
		c.n.expanded = true
		used += cc
		if c.n.glance {
			c.n.glance = false
			used--
		}
		inherited[c.n] = min(0, c.inherited+priority(c.n))
		push(c.n, c.depth+1, inherited[c.n])
	}
	// a folder of projects: what's been worked on lately is what the agent
	// will most likely touch next, so open those with what's left
	if workspace {
		var recent []*node
		for _, k := range root.kids {
			if k.glance && k.dir && time.Since(k.mtime) < 7*24*time.Hour {
				recent = append(recent, k)
			}
		}
		sort.SliceStable(recent, func(i, j int) bool { return recent[i].mtime.After(recent[j].mtime) })
		recent = recent[:min(len(recent), 3)]
		for i, k := range recent {
			share := min((opt.Budget-used)/(len(recent)-i), 50)
			if share < cost(k, opt.PerDir)+2 {
				continue
			}
			sub := opt
			sub.Budget, sub.workspace = share, false
			k.glance = false
			used += expand(k, sub) - 1 // its listing replaces the glance line
			k.recent = true
		}
	}
	// spend what's left on one-line glances into folders that stayed closed
	sort.SliceStable(closed, func(i, j int) bool {
		if closed[i].depth != closed[j].depth {
			return closed[i].depth < closed[j].depth
		}
		return closed[i].score > closed[j].score
	})
	for _, c := range closed {
		if used >= opt.Budget {
			break
		}
		if len(c.n.kids) > 1 && !c.n.expanded {
			c.n.glance = true
			used++
		}
	}
	return used
}

// ------------------------------------------------------------ details

const maxLineCount = 4 << 20

// details fills line counts / binary flags / image sizes for printed files.
func details(root *node, opt Options) {
	var files []*node
	var walk func(*node)
	walk = func(n *node) {
		kids, _ := shown(n, opt.PerDir)
		for _, k := range kids {
			if k.dir {
				if k.expanded {
					walk(k)
				}
				if k.project == nil && k.series == nil && !k.skipped && k.files > 0 {
					k.project = projectOf(k)
				}
			} else if k.series != nil {
				for i, m := range k.series {
					if i < 200 && m.target == "" && m.mode.IsRegular() {
						files = append(files, m)
					}
				}
			} else if k.target == "" && k.mode.IsRegular() {
				files = append(files, k)
			}
		}
	}
	walk(root)
	fsx.ParallelMin(len(files), 8, func(i int) {
		f := files[i]
		ext := extKey(f.name)
		if img.Supported(ext) {
			if w, h, err := img.Dimensions(f.abs); err == nil {
				f.binary, f.note = true, fmt.Sprintf("%dx%d", w, h)
				return
			}
		}
		if f.size > maxLineCount {
			f.binary = !looksText(f.abs)
			return
		}
		data, err := os.ReadFile(f.abs)
		if err != nil {
			return
		}
		if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
			f.binary = true
			return
		}
		f.lines = bytes.Count(data, []byte{'\n'})
		if len(data) > 0 && data[len(data)-1] != '\n' {
			f.lines++
		}
		if opt.Outline && HasOutline(f.name) {
			f.symbols, f.refs = outlineRefs(f.abs, 40)
		}
	})
}

func looksText(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8000)
	n, _ := f.Read(buf)
	return bytes.IndexByte(buf[:n], 0) < 0
}

func projectOf(n *node) *project.Info {
	names := make([]string, 0, len(n.kids))
	for _, k := range n.kids {
		names = append(names, k.name)
	}
	if n.sub {
		names = append(names, ".git")
	}
	p := project.Detect(n.abs, names)
	if p != nil && (p.Stack == "git repo" || p.Detail == "scripts") {
		return nil // too weak to be worth tokens
	}
	return p
}

// ------------------------------------------------------------ output

func human(n int64) string {
	const u = "KMGTP"
	if n < 1024 {
		return strconv.FormatInt(n, 10) + "B"
	}
	f := float64(n)
	i := -1
	for f >= 1024 && i < len(u)-1 {
		f /= 1024
		i++
	}
	if f < 10 {
		return strconv.FormatFloat(f, 'f', 1, 64) + string(u[i])
	}
	return strconv.FormatFloat(f, 'f', 0, 64) + string(u[i])
}

func count(n int) string {
	s := strconv.Itoa(n)
	if n < 10000 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(int(d.Minutes()), 0))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 730*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/24/365))
}

func quote(name string) string {
	if strings.ContainsAny(name, " \t\"'") {
		return strconv.Quote(name)
	}
	return name
}

func gitMark(s git.Status) string {
	if s.Clean() || s.Ignored() {
		return ""
	}
	x, y := s.X, s.Y
	if x == 0 || x == ' ' {
		x = '.'
	}
	if y == 0 || y == ' ' {
		y = '.'
	}
	if x == '?' {
		return " [new]"
	}
	return " [" + strings.Trim(string([]byte{x, y}), ".") + "]"
}

func topExts(m map[string]int, n int) string {
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].v != list[j].v {
			return list[i].v > list[j].v
		}
		return list[i].k < list[j].k
	})
	if len(list) > n && list[0].v == 1 {
		return fmt.Sprintf("%d different file types", len(list))
	}
	var parts []string
	for i, e := range list {
		if i == n || (i > 0 && e.v == 1 && len(list) > n) {
			break // don't present the first of many one-offs as if they were typical
		}
		parts = append(parts, fmt.Sprintf("%s×%d", e.k, e.v))
	}
	if len(parts) < n && len(list) > len(parts) { // one-offs were cut: say how many
		parts = append(parts, fmt.Sprintf("+%d more types", len(list)-len(parts)))
	}
	return strings.Join(parts, " ")
}

func dirLine(n *node, depth int, opt Options) string {
	name := quote(n.name) + "/"
	if n.series != nil {
		name = quote(n.series[0].name) + "/ … " + quote(n.series[len(n.series)-1].name) + "/"
	}
	var meta []string
	switch {
	case n.series != nil:
		meta = append(meta, fmt.Sprintf("%d more dirs like %s/; %s files, %s", len(n.series), quote(n.like), count(n.files), human(n.size)))
	case n.empty:
		meta = append(meta, "empty")
	case n.hollow != "":
		meta = append(meta, "no files listed: "+n.hollow)
	case n.gitlink:
		meta = append(meta, "nested git repo, not listed by the outer repo")
	case n.skipped:
		meta = append(meta, "not scanned")
	default:
		word := "files"
		if n.files == 1 {
			word = "file"
		}
		meta = append(meta, fmt.Sprintf("%s %s, %s", count(n.files), word, human(n.size)))
	}
	if n.partial {
		meta = append(meta, "PARTIAL: scan time limit hit, counts incomplete")
	}
	if n.project != nil {
		meta = append(meta, n.project.Label())
	}
	if n.repo != nil {
		meta = append(meta, repoText(n.repo))
	} else if n.sub && !n.gitlink {
		meta = append(meta, "own git repo")
	}
	if n.vendored && vendorDirs[strings.ToLower(n.name)] {
		meta = append(meta, "vendored/third-party")
	}
	if !n.expanded && !n.skipped && len(n.exts) > 0 {
		meta = append(meta, topExts(n.exts, 3))
	}
	if t := when(n, depth, opt); t != "" {
		meta = append(meta, t)
	}
	if n.commit != nil {
		meta = append(meta, commitText(n.commit))
	}
	if n.recent {
		meta = append(meta, "opened: recently active")
	}
	return name + " (" + strings.Join(meta, "; ") + ")" + dirMark(n)
}

// dirMark summarises uncommitted changes below a folder.
func dirMark(n *node) string {
	switch {
	case n.changedN == 0, n.repo != nil: // a repo's own count (incl. deletions) is in its line
		return ""
	case n.newN == n.files:
		return " [all new]"
	}
	return fmt.Sprintf(" [%d changed]", n.changedN)
}

func repoText(nr *nestedRepo) string {
	r := nr.repo
	t := "own git repo: " + r.Branch
	switch {
	case r.NoCommits:
		t += ", no commits yet"
	case nr.changes == 0:
		t += ", clean"
	default:
		t += fmt.Sprintf(", %d uncommitted", nr.changes)
	}
	if nr.last != nil {
		t += ", last commit " + age(nr.last.Time) + " ago"
	}
	return t
}

// when is the age (and with -l the mode) of an entry: always for the top
// level, everywhere with -l.
func when(n *node, depth int, opt Options) string {
	if !opt.Long && depth > 0 {
		return ""
	}
	var parts []string
	if opt.Long {
		mode := n.mode
		if n.dir && n.series == nil {
			if st, err := os.Lstat(n.abs); err == nil {
				mode = st.Mode()
			}
		}
		if mode != 0 {
			parts = append(parts, mode.String())
		}
	}
	// top-level files only carry an age when it's news (last 7 days); folders always do
	recentFile := n.dir || opt.Long || time.Since(n.mtime) < 7*24*time.Hour
	if !n.mtime.IsZero() && recentFile && (opt.sameAge == "" || depth > 0 || age(n.mtime) != opt.sameAge) {
		label := "modified "
		if n.dir && !n.empty && n.hollow == "" && n.files > 0 {
			label = "newest file "
		}
		if opt.Long { // exact, like ls -l, plus the relative age
			parts = append(parts, label+n.mtime.Format("2006-01-02 15:04")+" ("+age(n.mtime)+" ago)")
		} else {
			parts = append(parts, label+age(n.mtime)+" ago")
		}
	}
	return strings.Join(parts, ", ")
}

func commitText(c *git.Commit) string {
	subj := c.Subject
	if r := []rune(subj); len(r) > 50 {
		subj = string(r[:49]) + "…"
	}
	return "last commit " + age(c.Time) + " ago: " + subj
}

func fileLine(n *node, depth int, opt Options) string {
	var b strings.Builder
	if n.series != nil {
		b.WriteString(quote(n.name) + " ×" + strconv.Itoa(len(n.series)) + " " + seriesLines(n))
		if t := when(n, depth, opt); t != "" {
			b.WriteString(" (" + t + ")")
		}
		b.WriteString(gitMark(n.git))
		return b.String()
	}
	b.WriteString(quote(n.name))
	switch {
	case n.target != "":
		b.WriteString(" -> " + n.target)
	case n.note != "":
		b.WriteString(" " + n.note + " " + human(n.size))
	case n.binary:
		b.WriteString(" bin " + human(n.size))
	default:
		b.WriteString(" " + count(n.lines) + "L")
		switch {
		case opt.Long:
			b.WriteString(" " + strconv.FormatInt(n.size, 10) + "B") // exact bytes, like ls -l
		case n.size >= 100<<10:
			b.WriteString(" " + human(n.size))
		}
	}
	if n.mode&0o111 != 0 && n.mode.IsRegular() {
		b.WriteString(" exec")
	}
	var extra []string
	if t := when(n, depth, opt); t != "" {
		extra = append(extra, t)
	}
	if n.commit != nil {
		extra = append(extra, commitText(n.commit))
	}
	if len(extra) > 0 {
		b.WriteString(" (" + strings.Join(extra, "; ") + ")")
	}
	b.WriteString(gitMark(n.git))
	return b.String()
}

// changedLine lists the changed files inside a collapsed folder (--changed).
func changedLine(n *node) string {
	var paths []string
	var walk func(*node)
	walk = func(d *node) {
		for _, k := range d.kids {
			switch {
			case k.dir:
				walk(k)
			case k.series != nil:
				for _, m := range k.series {
					paths = append(paths, strings.TrimPrefix(m.rel, n.rel+"/")+gitMark(m.git))
				}
			default:
				paths = append(paths, strings.TrimPrefix(k.rel, n.rel+"/")+gitMark(k.git))
			}
		}
	}
	walk(n)
	line, used := "", 0
	for i, p := range paths {
		if used+len(p) > 200 {
			line += fmt.Sprintf(" +%d more", len(paths)-i)
			break
		}
		if i > 0 {
			line += ", "
		}
		line += p
		used += len(p) + 2
	}
	return "↳ " + line
}

// glanceLine lists a collapsed folder's direct children by name.
func glanceLine(n *node) string {
	var names []string
	for _, k := range n.kids {
		if k.skipped || k.name == ".git" {
			continue // build output, caches, VCS internals: not worth the tokens here
		}
		switch {
		case k.series != nil && k.dir:
			names = append(names, k.series[0].name+"/…"+k.series[len(k.series)-1].name+"/")
		case k.dir:
			names = append(names, quote(k.name)+"/")
		case k.series != nil:
			names = append(names, quote(k.name))
		default:
			names = append(names, quote(k.name))
		}
	}
	line, used := "", 0
	for i, nm := range names {
		if used+len(nm) > 150 {
			line += fmt.Sprintf(" +%d", len(names)-i)
			break
		}
		if i > 0 {
			line += " "
		}
		line += nm
		used += len(nm) + 1
	}
	return "↳ " + line
}

func render(w io.Writer, n *node, depth int, opt Options) (collapsed int) {
	kids, rest := shown(n, opt.PerDir)
	indent := strings.Repeat("  ", depth)
	glances := map[string]string{} // glance text → first sibling that had it
	for _, k := range kids {
		if k.dir {
			fmt.Fprintln(w, indent+dirLine(k, depth, opt))
			switch {
			case k.expanded:
				collapsed += render(w, k, depth+1, opt)
			case k.glance:
				if opt.Changed {
					fmt.Fprintln(w, indent+"  "+changedLine(k))
				} else if g := glanceLine(k); glances[g] != "" {
					fmt.Fprintln(w, indent+"  ↳ same layout as "+glances[g]+"/")
				} else {
					glances[g] = quote(k.name)
					fmt.Fprintln(w, indent+"  "+g)
				}
				collapsed++
			case expandable(k):
				collapsed++
			}
			continue
		}
		fmt.Fprintln(w, indent+fileLine(k, depth, opt))
		if len(k.symbols) > 0 {
			var parts []string
			used, dropped := 0, 0
			for i, s := range k.symbols {
				if s.Kind == "define" {
					continue
				}
				p := fmt.Sprintf("%s:%d", k.refs[i], s.Line)
				if used+len(p) > 220 {
					dropped++
					continue
				}
				parts = append(parts, p)
				used += len(p) + 1
			}
			line := strings.Join(parts, " ")
			if dropped > 0 {
				line += fmt.Sprintf(" +%d more (glint -A <file>)", dropped)
			}
			fmt.Fprintln(w, indent+"  ↳ "+line)
		}
	}
	if rest > 0 {
		in := map[*node]bool{}
		for _, k := range kids {
			in[k] = true
		}
		left := map[string]int{}
		var dirNames, fileNames []string
		for _, k := range n.kids {
			if in[k] {
				continue
			}
			if k.dir {
				dirNames = append(dirNames, quote(k.name)+"/")
			} else {
				left[extKey(k.name)]++
				fileNames = append(fileNames, quote(k.name))
			}
		}
		// the top level is always complete: every omitted name is listed.
		// deeper down, omitted folders are named and files summarised.
		if depth == 0 {
			fmt.Fprintln(w, "… also: "+strings.Join(append(dirNames, fileNames...), " "))
		} else {
			var parts []string
			if len(fileNames) > 0 {
				parts = append(parts, fmt.Sprintf("+%d files (%s)", len(fileNames), topExts(left, 4)))
			}
			if len(dirNames) > 0 {
				names := dirNames
				if len(names) > 15 {
					names = append(names[:15:15], fmt.Sprintf("+%d more", len(dirNames)-15))
				}
				parts = append(parts, fmt.Sprintf("+%d dirs: %s", len(dirNames), strings.Join(names, " ")))
			}
			fmt.Fprintln(w, indent+"… "+strings.Join(parts, ", "))
		}
	}
	return collapsed
}

func listDir(w io.Writer, dir string, opt Options) {
	s := gather(dir, opt)
	if opt.Changed && s.repo == nil && len(s.repos) == 0 {
		fmt.Fprintf(w, "%s\n--changed needs git: this folder is not in a git repo and contains none. Use --since 2h for recently modified files.\n", s.rootAbs)
		return
	}
	root, total := build(s, opt)
	for _, k := range root.kids {
		if k.dir && k.series == nil && !k.skipped && k.files > 0 {
			k.project = projectOf(k)
		}
	}
	const headerLines = 6 // the header is part of what the agent reads
	treeOpt := opt
	treeOpt.Budget = max(opt.Budget-headerLines, 10)
	treeOpt.workspace = s.repo == nil && projectOf(root) == nil
	expand(root, treeOpt)
	if opt.Since == 0 && !opt.Changed {
		addEmptyDirs(root, opt)
	}
	if opt.Commits && s.repo != nil {
		var names []string
		for _, k := range root.kids {
			if !k.skipped {
				names = append(names, k.name)
			}
		}
		hist := s.repo.History(s.rootAbs, names, false)
		for _, k := range root.kids {
			k.commit = hist[k.name]
		}
	}
	details(root, opt)

	// in a fresh clone everything has the same age; say it once, not per line
	ages := map[string]int{}
	for _, k := range root.kids {
		if !k.mtime.IsZero() {
			ages[age(k.mtime)]++
		}
	}
	for a, c := range ages {
		if len(root.kids) > 3 && c*5 >= len(root.kids)*4 && !opt.Long {
			opt.sameAge = a
		}
	}

	// line 1: where, what, git state
	head := s.rootAbs
	var facts []string
	if p := projectOf(root); p != nil {
		facts = append(facts, p.Label())
	}
	if r := s.repo; r != nil {
		g := "git " + r.Branch
		if r.Ahead > 0 {
			g += fmt.Sprintf(" ahead %d", r.Ahead)
		}
		if r.Behind > 0 {
			g += fmt.Sprintf(" behind %d", r.Behind)
		}
		inRepo := len(s.changes)
		if r.Root != s.rootAbs { // s.repo's status was scoped to this folder
			if full, err := git.Open(r.Root); err == nil && full != nil {
				inRepo = len(full.Changes(r.Root))
			}
		}
		switch {
		case r.NoCommits:
			g += fmt.Sprintf(", no commits yet (%d files untracked/staged)", inRepo)
		case r.Root != s.rootAbs:
			g += fmt.Sprintf(", %d uncommitted here, %d in the whole repo", len(s.changes), inRepo)
		case inRepo == 0:
			g += " clean"
		default:
			g += fmt.Sprintf(" %d uncommitted", inRepo)
		}
		if c := r.LastCommit(); c != nil {
			g += fmt.Sprintf("; HEAD %s %s ago: %s", c.Hash, age(c.Time), c.Subject)
		}
		if r.Root != s.rootAbs {
			g += "; repo root " + r.Root
		}
		facts = append(facts, g)
	}
	if len(facts) > 0 {
		head += " — " + strings.Join(facts, " · ")
	}
	fmt.Fprintln(w, head)
	if opt.Since == 0 && !opt.Changed && (s.repo != nil || projectOf(root) != nil) {
		if about := readmeSummary(s.rootAbs); about != "" {
			fmt.Fprintln(w, "about: "+about)
		}
	}
	if s.note != "" {
		fmt.Fprintln(w, "note: "+s.note)
	}

	// line 2: what the numbers mean
	word := "files"
	if total == 1 {
		word = "file"
	}
	sum := fmt.Sprintf("%s %s, %s", count(total), word, human(root.size))
	switch {
	case s.repo != nil:
		sum += " (what git sees: tracked + untracked, excluding ignored)"
	case len(s.repos) > 0:
		sum += fmt.Sprintf(" (source view: each of the %d git repos counted as git sees it, ignored files excluded; build output and caches outside git skipped; real disk use: glint -A -M)", len(s.repos))
	default:
		sum += " (build output, caches and dot-folders skipped; real disk use: glint -A -M)"
	}
	if opt.Since > 0 {
		sum += " modified in the last " + opt.SinceRaw
	}
	if opt.Changed {
		sum += " with uncommitted changes"
		if s.repo == nil {
			dirty := 0
			for _, nr := range s.repos {
				if nr.changes > 0 || nr.repo.NoCommits {
					dirty++
				}
			}
			sum += fmt.Sprintf(": %d of the %d git repos below have changes", dirty, len(s.repos))
		}
	}
	if opt.sameAge != "" {
		sum += "; top-level entries last modified " + opt.sameAge + " ago unless noted"
	} else if !opt.Long {
		sum += "; top-level files show their age only if under 7 days, folders always show their newest file"
	}
	if s.partial {
		sum += "; SCAN STOPPED EARLY (time/size limit): totals are partial"
	}
	fmt.Fprintln(w, sum)

	if r := s.repo; r != nil && !r.NoCommits {
		if files, n := r.HeadFiles(6); n > 0 {
			line := "HEAD touched: " + strings.Join(files, ", ")
			if n > len(files) {
				line += fmt.Sprintf(" +%d more", n-len(files))
			}
			fmt.Fprintln(w, line)
		}
	}
	if len(s.changes) > 0 && !opt.Changed {
		var parts []string
		for i, c := range s.changes {
			if i == 15 {
				parts = append(parts, fmt.Sprintf("+%d more", len(s.changes)-15))
				break
			}
			parts = append(parts, strings.TrimSpace(gitMark(c.Status))+" "+quote(c.Path))
		}
		fmt.Fprintln(w, "changed: "+strings.Join(parts, ", "))
	}
	if len(s.ignored) > 0 {
		list := groupIgnored(s.ignored)
		extra := ""
		if len(list) > 12 {
			extra = fmt.Sprintf(", +%d more", len(list)-12)
			list = list[:12]
		}
		fmt.Fprintln(w, "gitignored (hidden): "+strings.Join(list, ", ")+extra)
	}
	if len(s.repos) == 0 && !opt.Changed && opt.Since == 0 {
		if line := largestSource(root); line != "" {
			fmt.Fprintln(w, line)
		}
	}
	fmt.Fprintln(w)

	if total == 0 && len(root.kids) == 0 {
		fmt.Fprintln(w, "(nothing to show)")
		return
	}
	collapsed := render(w, root, 0, opt)
	if collapsed > 0 {
		fmt.Fprintf(w, "\n%d dirs not expanded (budget %d lines; the top level is always listed in full); `glint -A <dir>` opens one.\n", collapsed, opt.Budget)
	}
}

var (
	mdLinkRe  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdTagRe   = regexp.MustCompile(`<[^>]+>`)
	mdEmphRe  = regexp.MustCompile(`(\*\*|__|\*|_)([^*_\s][^*_]*?)(\*\*|__|\*|_)`)
	mdSkipRe  = regexp.MustCompile(`^(#|<|\[!\[|!\[|\x60\x60\x60|---|===|\||>|\[.*\]:|\s*[-*+] )`)
	readmeRes = []string{"README.md", "readme.md", "Readme.md", "README", "README.rst", "README.txt", "README.markdown"}
)

// readmeSummary returns the first prose paragraph of the README, so an
// agent learns what a project is without reading the file.
func readmeSummary(dir string) string {
	var data []byte
	for _, name := range readmeRes {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			data = b[:min(len(b), 16<<10)]
			break
		}
	}
	if data == nil {
		return ""
	}
	var para []string
	inFence := false
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "\x60\x60\x60") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if t == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		if mdSkipRe.MatchString(t) {
			if len(para) > 0 {
				break
			}
			continue
		}
		para = append(para, t)
	}
	text := strings.Join(para, " ")
	text = mdLinkRe.ReplaceAllString(text, "$1")
	text = mdTagRe.ReplaceAllString(text, "")
	text = mdEmphRe.ReplaceAllString(text, "$2")
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > 220 {
		cut := string(r[:220])
		if i := strings.LastIndexAny(cut, ".!?"); i > 80 {
			cut = cut[:i+1]
		} else if i := strings.LastIndex(cut, " "); i > 0 {
			cut = cut[:i] + "…"
		}
		text = cut
	}
	return text
}

var sourceExts = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`go rs py pyi js mjs cjs ts mts tsx jsx c h cc cpp cxx hpp hh cs java kt kts rb php lua
		sh bash zsh fish swift zig dart vue svelte astro gd gdshader glsl wgsl hlsl sql ex exs hs ml scala clj el r jl m mm`) {
		sourceExts[e] = true
	}
}

func generatedName(name string) bool {
	l := strings.ToLower(name)
	for _, p := range []string{".min.", ".pb.", "_pb2", ".generated.", "_generated", ".gen.", "bindings."} {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// largestSource names the biggest hand-written source files (vendored and
// generated code excluded): "where's the meat" in one line.
func largestSource(root *node) string {
	var cands []*node
	var walk func(*node)
	walk = func(n *node) {
		for _, k := range n.kids {
			switch {
			case k.vendored || k.skipped:
			case k.dir:
				walk(k)
			case k.series != nil:
				for _, m := range k.series {
					if sourceExts[extKey(m.name)] && !generatedName(m.name) {
						cands = append(cands, m)
					}
				}
			case k.target == "" && sourceExts[extKey(k.name)] && !generatedName(k.name):
				cands = append(cands, k)
			}
		}
	}
	walk(root)
	if len(cands) < 2 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].size > cands[j].size })
	cands = cands[:min(len(cands), 8)]
	fsx.Parallel(len(cands), func(i int) {
		if cands[i].lines == 0 && !cands[i].binary {
			if data, err := os.ReadFile(cands[i].abs); err == nil && bytes.IndexByte(data[:min(len(data), 8000)], 0) < 0 {
				cands[i].lines = bytes.Count(data, []byte{'\n'})
			}
		}
	})
	sort.Slice(cands, func(i, j int) bool { return cands[i].lines > cands[j].lines })
	var parts []string
	for _, c := range cands[:min(len(cands), 5)] {
		parts = append(parts, fmt.Sprintf("%s %sL", quote(c.rel), count(c.lines)))
	}
	return "largest source files (excl. vendored/generated): " + strings.Join(parts, ", ")
}

// groupIgnored folds ignored paths that live under the same top-level
// folder into one "folder/ (N ignored)" entry.
func groupIgnored(paths []string) []string {
	groups := map[string][]string{}
	var order []string
	for _, p := range paths {
		top, rest, nested := strings.Cut(p, "/")
		key := p
		if nested && rest != "" {
			key = top + "/"
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], p)
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		g := groups[k]
		switch {
		case len(g) == 1:
			out = append(out, g[0])
		case groups[k][0] == k: // the folder itself is ignored
			out = append(out, k)
		default:
			out = append(out, fmt.Sprintf("%s (%d ignored inside)", k, len(g)))
		}
	}
	return out
}

// describeFile is the single-file view: facts plus an outline with line
// numbers. here returns the working directory's repository (nil with
// --no-git).
func describeFile(w io.Writer, path string, st fs.FileInfo, opt Options, here func() *repo.Info) {
	abs, _ := filepath.Abs(path)
	n := &node{name: filepath.Base(abs), abs: abs, mode: st.Mode(), size: st.Size(), mtime: st.ModTime()}
	for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(abs)), "/") {
		if vendorDirs[strings.ToLower(part)] {
			n.vendored = true
		}
	}
	if lst, err := os.Lstat(abs); err == nil && lst.Mode()&fs.ModeSymlink != 0 {
		n.target, _ = os.Readlink(abs)
	}
	var repo *git.Repo
	if !opt.NoGit {
		repo, _ = git.Open(filepath.Dir(abs))
	}
	if repo != nil {
		n.git = repo.Status(abs)
		if !n.git.Untracked() && !n.git.Ignored() {
			hist := repo.History(filepath.Dir(abs), []string{n.name}, false)
			n.commit = hist[n.name]
		}
	}
	opt.Outline = true
	parent := &node{dir: true, kids: []*node{n}, expanded: true}
	details(parent, opt)
	line := headerPath(abs, here) + " — " + strings.TrimPrefix(fileLine(n, 1, Options{}), quote(n.name)+" ")
	if opt.Long {
		line += ", " + n.mode.String()
	}
	line += ", modified " + age(n.mtime) + " ago"
	if n.git.Ignored() {
		line += ", gitignored"
	}
	if n.vendored {
		line += ", VENDORED third-party code"
	}
	fmt.Fprintln(w, line)
	if len(n.symbols) == 0 {
		return
	}
	const maxSyms = 150
	syms := Outline(abs, wholeFile)
	names := refNames(syms)
	var defines []string
	shownN := 0
	for i, s := range syms {
		if s.Kind == "define" {
			defines = append(defines, names[i])
			continue
		}
		if shownN == maxSyms {
			shownN++
			continue
		}
		if shownN < maxSyms {
			fmt.Fprintf(w, "  %d %s\n", s.Line, symbolLine(s, names[i]))
		}
		shownN++
	}
	if shownN > maxSyms {
		fmt.Fprintf(w, "  … +%d more symbols; grep the file or read it with an offset\n", shownN-maxSyms)
	}
	if len(defines) > 0 {
		list := defines
		extra := ""
		if len(list) > 40 {
			extra = fmt.Sprintf(" +%d more", len(list)-40)
			list = list[:40]
		}
		fmt.Fprintf(w, "  #defines (%d): %s%s\n", len(defines), strings.Join(list, " "), extra)
	}
}

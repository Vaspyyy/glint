package agent

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/prism/repo"
	"github.com/Vaspyyy/prism/work"
)

// The work context (prism/work, docs/context.md in Prism): what changed in
// the repository, uncommitted or on this branch. The views that show code
// structure (the map and the outline) mark changed files and symbols with
// it and say so in a context: line at the top. Plain listings never load
// it. It only marks: it never changes which entries are listed.
//
// glint reads one layer, the changed files and symbols. Files with
// uncommitted changes already carry their git status ([M], [new], ...),
// which says more than [changed], so the map only adds [branch]: files
// committed on this branch since it left the default branch, which look
// clean to git status. Symbols get both marks.

// workCtx is one repository's context.
type workCtx struct {
	info *repo.Info  // nil, or Git false, outside a repository
	st   *work.State // never nil
}

// rel returns abs relative to the repository root, the form prism's marks
// use; ok is false outside the repository.
func (c *workCtx) rel(abs string) (string, bool) {
	if c == nil || c.info == nil || !c.info.Git || c.st.Changes == nil {
		return "", false
	}
	return c.info.Rel(abs)
}

// fileMarks returns what the map prints after a file (repo-relative): " [branch]"
// when it changed only on this branch, else "" (uncommitted changes show
// as its git status).
func (c *workCtx) fileMarks(rel string) string {
	if c == nil || c.st.Changes.FileMark(rel) != work.MarkBranch {
		return ""
	}
	return work.Suffix(c.st.FileMarks(rel))
}

// marks returns what is printed after a symbol: " [changed]", " [branch]"
// or "". ref is the symbol's full ref.
func (c *workCtx) marks(ref string) string {
	if c == nil {
		return ""
	}
	return work.Suffix(c.st.Marks(ref))
}

// localMarks reports whether a file with this git status can show a mark
// for its uncommitted changes: on its symbols, unless it's new, and only if
// it's still there. (Its file mark is its git status.)
func localMarks(s git.Status) bool {
	return !s.Clean() && !s.Ignored() && !isNew(s) && s.X != 'D' && s.Y != 'D'
}

// isNew reports whether every line of a file is new: it's untracked or
// added. Its git status ([new], [A]) says so, so the text views leave
// its symbols unmarked rather than marking every one of them.
func isNew(s git.Status) bool { return s.Untracked() || s.X == 'A' }

// contexts loads the context of each repository a run lists, once per
// repository, in the background.
type contexts struct {
	off   bool
	mu    sync.Mutex
	roots map[string]func() *workCtx
	used  []*workCtx // in the order they were first waited for: one context: line each
}

// newContexts is a run's contexts. With off (--no-context,
// PRISM_NO_CONTEXT=1 or --no-git) nothing is loaded and nothing marked.
func newContexts(off bool) *contexts {
	return &contexts{off: off, roots: map[string]func() *workCtx{}}
}

// start begins loading the context of the repository containing path (a
// directory or file glint lists) and returns a function that waits for
// it. It runs git, so it runs next to glint's own work. here, if not nil,
// returns the working directory's repository, which is looked up anyway:
// when path is in it too, its lookup is shared rather than run twice.
//
// quiet, if not nil, waits for glint's own git status of path and reports
// whether no uncommitted change in it can show a mark (see localMarks).
// When this branch's commits didn't change path either, nothing listed
// can get a mark, so the changes aren't loaded. That saves a git status
// of the whole repository, which in a big one costs more than everything
// else glint -A <file> does (in a 31,000-file repository about 60 ms,
// against 20 ms).
func (cs *contexts) start(path string, dir bool, here func() *repo.Info, quiet func() bool) func() *workCtx {
	if cs.off {
		c := &workCtx{st: work.Load(nil, work.Changes, true)}
		return func() *workCtx { return c }
	}
	parent := path
	if !dir {
		parent = filepath.Dir(path)
	}
	if abs, err := filepath.Abs(parent); err == nil && git.FindRoot(abs) == "" {
		c := &workCtx{st: work.Load(nil, work.Changes, false)} // not in a repository: no git to run
		return func() *workCtx { return c }
	}
	if here == nil || !sameRepo(".", parent) {
		here = func() *repo.Info {
			info, err := repo.Detect(parent)
			if err != nil {
				return nil
			}
			return info
		}
	}
	ch := make(chan func() *workCtx, 1)
	go func() {
		info := here()
		if info == nil || !info.Git {
			c := &workCtx{info: info, st: work.Load(info, work.Changes, false)}
			ch <- func() *workCtx { return c }
			return
		}
		if quiet != nil && !branchTouched(info, path) && quiet() {
			c := &workCtx{info: info, st: work.Load(info, 0, false)}
			ch <- func() *workCtx { return c }
			return
		}
		cs.mu.Lock()
		load, ok := cs.roots[info.Root]
		if !ok {
			load = sync.OnceValue(func() *workCtx {
				return &workCtx{info: info, st: work.Load(info, work.Changes, false)}
			})
			cs.roots[info.Root] = load
		}
		cs.mu.Unlock()
		load()
		ch <- load
	}()
	return sync.OnceValue(func() *workCtx {
		c := (<-ch)()
		cs.mu.Lock()
		if !slices.Contains(cs.used, c) {
			cs.used = append(cs.used, c)
		}
		cs.mu.Unlock()
		return c
	})
}

// branchTouched reports whether the commits prism/changes marks [branch]
// changed anything at path: on a branch other than the default one, the
// commits since the merge base with the default branch. Two small git
// commands, against prism's git status of the whole repository.
func branchTouched(info *repo.Info, path string) bool {
	if info.Branch == "" || info.DefaultRef == "" || info.Branch == info.DefaultBranch || info.Head == "" {
		return false
	}
	rel, ok := info.Rel(path)
	if !ok {
		return true
	}
	base := git.MergeBase(info.Root, "HEAD", info.DefaultRef)
	return base != "" && base != info.Head && git.Differs(info.Root, base, "HEAD", rel)
}

// flag is a bool that is set once and can be waited for.
type flag struct {
	once sync.Once
	done chan struct{}
	v    bool
}

func newFlag() *flag { return &flag{done: make(chan struct{})} }

// set sets the flag; only the first call counts.
func (f *flag) set(v bool) {
	f.once.Do(func() {
		f.v = v
		close(f.done)
	})
}

// wait waits for the flag to be set and returns it.
func (f *flag) wait() bool {
	<-f.done
	return f.v
}

// sameRepo reports whether two directories are in the same work tree,
// without running git: the nearest .git above them is the same.
func sameRepo(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	root := git.FindRoot(absA)
	return root != "" && root == git.FindRoot(absB)
}

// lines returns the context: lines to print at the top, once everything
// has been rendered.
func (cs *contexts) lines() []string {
	var out []string
	for _, c := range cs.used {
		if l := c.st.Line(); l != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// markTree marks the files of a map that changed only on this branch, and
// counts them in every folder above them; a folded run of entries carries
// its members' marks. prefix is the listing root relative to the
// repository root. It returns whether a listed file with an outline
// changed (so --outline may mark its symbols) and whether a file got a
// mark.
func markTree(root *node, c *workCtx, prefix string) (changed, marked bool) {
	if c == nil || c.st.Changes == nil {
		return false, false
	}
	for _, f := range c.st.Changes.Files {
		rel := f
		if prefix != "." {
			var ok bool
			if rel, ok = strings.CutPrefix(f, prefix+"/"); !ok {
				continue
			}
		}
		var dirs []*node
		n := root
		for part := range strings.SplitSeq(rel, "/") {
			if n == nil || !n.dir {
				n = nil
				break
			}
			dirs = append(dirs, n)
			n = n.byName[part]
		}
		if n == nil || (n.dir && !n.gitlink) {
			continue // not listed: filtered out, deleted, or inside a folder that wasn't scanned
		}
		changed = changed || (HasOutline(n.name) && !isNew(n.git))
		if n.marks = c.fileMarks(f); n.marks != "" {
			marked = true
			if s := n.inSeries; s != nil && s.marks == "" {
				s.marks = n.marks
			}
			for _, d := range dirs {
				d.branchN++
				if s := d.inSeries; s != nil {
					s.branchN++
				}
			}
		}
	}
	return changed, marked
}

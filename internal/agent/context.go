package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/prism/changes"
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

// fileMarks returns what the text views print after a file (repo-relative):
// " [branch]" when it changed only on this branch, else "" (uncommitted
// changes show as its git status).
func (c *workCtx) fileMarks(rel string) string {
	if c == nil {
		return ""
	}
	m := c.st.PeekFile(rel)
	if !slices.Equal(m, []string{work.MarkBranch}) {
		return ""
	}
	c.st.Show(m...)
	return work.Suffix(m)
}

// marks returns what is printed after a symbol: " [changed]", " [branch]"
// or "". ref is the symbol's full ref.
func (c *workCtx) marks(ref string) string {
	if c == nil {
		return ""
	}
	return work.Suffix(c.st.Marks(ref))
}

// isNew reports whether every line of a file is new: it's untracked or
// added. Its git status ([new], [A]) says so, so the text views leave
// its symbols unmarked rather than marking every one of them.
func isNew(s git.Status) bool { return s.Untracked() || s.X == 'A' }

// contexts loads the context of the repositories a run lists: one load
// per repository, scoped to the paths listed in it, so a big repository's
// git status only looks at those. A map without an outline only needs
// file marks, so it skips the hunks and outlines.
type contexts struct {
	off     bool
	outline bool // maps show symbols (--outline), so they need symbol detail too
	byRoot  map[string]*group
	groups  []*group
	mu      sync.Mutex
	used    []*workCtx // in the order they were first waited for: one context: line each
}

// group is the paths of one repository.
type group struct {
	paths []string          // absolute
	files bool              // a file is listed: its symbols are shown
	here  func() *repo.Info // the working directory's repository, when it's this one
	get   func() *workCtx
}

// newContexts is a run's contexts. With off (--no-context,
// PRISM_NO_CONTEXT=1 or --no-git) nothing is loaded and nothing marked.
func newContexts(off, outline bool) *contexts {
	return &contexts{off: off, outline: outline, byRoot: map[string]*group{}}
}

// add registers a listed path (a directory, or a file) and returns a
// function that waits for its repository's context, once start has run.
// here, if not nil, returns the working directory's repository, which is
// looked up anyway: when path is in it too, that lookup is shared rather
// than run twice.
func (cs *contexts) add(path string, dir bool, here func() *repo.Info) func() *workCtx {
	if cs.off {
		c := &workCtx{st: work.Load(nil, work.Changes, true)}
		return func() *workCtx { return c }
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	parent := abs
	if !dir {
		parent = filepath.Dir(abs)
	}
	root := git.FindRoot(parent)
	if root == "" {
		c := &workCtx{st: work.Load(nil, work.Changes, false)} // not in a repository: no git to run
		return func() *workCtx { return c }
	}
	g := cs.byRoot[root]
	if g == nil {
		g = &group{}
		cs.byRoot[root] = g
		cs.groups = append(cs.groups, g)
	}
	g.paths = append(g.paths, abs)
	g.files = g.files || !dir
	if here != nil && g.here == nil && sameRepo(".", parent) {
		g.here = here
	}
	return func() *workCtx { return g.get() }
}

// start begins loading every repository's context in the background: it
// runs git, so it runs next to glint's own work.
func (cs *contexts) start() {
	for _, g := range cs.groups {
		ch := make(chan *workCtx, 1)
		go func() { ch <- g.load(!g.files && !cs.outline) }()
		g.get = sync.OnceValue(func() *workCtx {
			c := <-ch
			cs.mu.Lock()
			cs.used = append(cs.used, c)
			cs.mu.Unlock()
			return c
		})
	}
}

// load reads the context of a group's repository, scoped to its paths.
func (g *group) load(filesOnly bool) *workCtx {
	var info *repo.Info
	if g.here != nil {
		info = g.here()
	} else {
		dir := g.paths[0]
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			dir = filepath.Dir(dir)
		}
		info, _ = repo.Detect(dir)
	}
	if info == nil || !info.Git {
		return &workCtx{info: info, st: work.Load(info, work.Changes, false)}
	}
	var paths []string
	for _, p := range g.paths {
		rel, ok := info.Rel(p)
		if !ok {
			continue
		}
		if rel == "." {
			paths = nil // the whole repository
			break
		}
		paths = append(paths, rel)
	}
	st := work.LoadWith(info, work.Options{Layers: work.Changes, Changes: changes.Options{Paths: paths, FilesOnly: filesOnly}})
	return &workCtx{info: info, st: st}
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

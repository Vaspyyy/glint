package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// workCtx is one repository's context, as loaded for some of its paths.
type workCtx struct {
	info    *repo.Info  // nil, or Git false, outside a repository
	st      *work.State // never nil
	cs      *contexts
	printed int // marks printed so far
}

// rel returns abs relative to the repository root, the form prism's marks
// use; ok is false outside the repository.
func (c *workCtx) rel(abs string) (string, bool) {
	if c == nil || c.info == nil || !c.info.Git || c.st.Changes == nil {
		return "", false
	}
	return c.info.Rel(abs)
}

// fileMarks returns the marks the text views give a file (repo-relative):
// "branch" when it changed only on this branch (uncommitted changes show
// as its git status). They count once they're printed (print).
func (c *workCtx) fileMarks(rel string) []string {
	if c == nil {
		return nil
	}
	if m := c.st.PeekFile(rel); slices.Equal(m, []string{work.MarkBranch}) {
		return m
	}
	return nil
}

// print returns marks as printed after a name (" [branch]") and counts
// them as shown: only what's printed goes in the context: line.
func (c *workCtx) print(marks []string) string {
	if len(marks) == 0 {
		return ""
	}
	if c != nil {
		c.st.Show(marks...)
		c.printed++
	}
	return suffix(marks)
}

// count is a folder's " [N branch]": N files below it changed only on this
// branch. It counts as shown.
func (c *workCtx) count(n int) string {
	c.print([]string{work.MarkBranch})
	return " [" + strconv.Itoa(n) + " " + work.MarkBranch + "]"
}

// marks returns what is printed after a symbol: " [changed]", " [branch]"
// or "". ref is the symbol's full ref.
func (c *workCtx) marks(ref string) string {
	if c == nil {
		return ""
	}
	m := c.st.Marks(ref)
	if len(m) > 0 {
		c.printed++
	}
	return suffix(m)
}

// claimLine reports whether the context: line still has to be fitted into
// a budget: the first map that prints a mark claims it, since there's one
// line for the whole run.
func (c *workCtx) claimLine() bool {
	if c == nil || c.cs == nil || c.cs.claimed {
		return false
	}
	c.cs.claimed = true
	return true
}

func suffix(marks []string) string { return work.Suffix(marks) }

// isNew reports whether every line of a file is new at its path: it's
// untracked, added (also with git add -N), renamed or copied. prism/changes
// doesn't follow renames, so it marks every symbol of such a file; its git
// status ([new], [A], [R], [C]) says that already, so the text views
// leave its symbols unmarked. JSON keeps the marks.
func isNew(s git.Status) bool {
	return s.Untracked() || s.X == 'A' || s.Y == 'A' || s.X == 'R' || s.X == 'C'
}

// contexts loads the context of the repositories a run lists, scoped to
// the paths listed in each, so a big repository's git status only looks
// at those. Files and folders of one repository load separately, in
// parallel: a file always gets symbol detail, while a map without an
// outline only needs file marks, and a big folder next to a file mustn't
// push the file's load over prism's limit for symbol detail.
type contexts struct {
	off     bool
	outline bool // maps show symbols (--outline), so they need symbol detail too
	byKey   map[string]*group
	groups  []*group
	mu      sync.Mutex
	used    []*workCtx // in the order they were first waited for
	claimed bool       // a map has fitted the context: line into its budget
}

// group is the listed files, or the listed folders, of one repository.
type group struct {
	paths []string          // absolute
	files bool              // files: their symbols are shown
	here  func() *repo.Info // the working directory's repository, when it's this one
	get   func() *workCtx
}

// newContexts is a run's contexts. With off (--no-context,
// PRISM_NO_CONTEXT=1 or --no-git) nothing is loaded and nothing marked.
func newContexts(off, outline bool) *contexts {
	return &contexts{off: off, outline: outline, byKey: map[string]*group{}}
}

// add registers a listed path (a directory, or a file) and returns a
// function that waits for its repository's context, once start has run.
// here, if not nil, returns the working directory's repository, which is
// looked up anyway: when path is in it too, that lookup is shared rather
// than run twice.
func (cs *contexts) add(path string, dir bool, here func() *repo.Info) func() *workCtx {
	if cs.off {
		c := &workCtx{st: work.Load(nil, work.Changes, true), cs: cs}
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
		c := &workCtx{st: work.Load(nil, work.Changes, false), cs: cs} // not in a repository: no git to run
		return func() *workCtx { return c }
	}
	key := root + "\x00dir"
	if !dir {
		key = root + "\x00file"
	}
	g := cs.byKey[key]
	if g == nil {
		g = &group{files: !dir}
		cs.byKey[key] = g
		cs.groups = append(cs.groups, g)
	}
	g.paths = append(g.paths, abs)
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
			c.cs = cs
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

// line returns the context: line to print at the top, once everything has
// been rendered, or "". There's one line for the run: when more than one
// repository contributed, each one's part starts with its folder's name.
func (cs *contexts) line() string {
	var roots []string
	parts := map[string][]string{}
	for _, c := range cs.used {
		p, ok := strings.CutPrefix(c.st.Line(), "context: ")
		if !ok || p == "" {
			continue
		}
		root := ""
		if c.info != nil {
			root = c.info.Root
		}
		if _, seen := parts[root]; !seen {
			roots = append(roots, root)
		}
		if !slices.Contains(parts[root], p) {
			parts[root] = append(parts[root], p)
		}
	}
	if len(roots) == 0 {
		return ""
	}
	var out []string
	for _, root := range roots {
		p := strings.Join(parts[root], "; ")
		if len(roots) > 1 {
			p = filepath.Base(root) + ": " + p
		}
		out = append(out, p)
	}
	return "context: " + strings.Join(out, "; ")
}

// markTree marks the files of a map that changed only on this branch, and
// counts them in every folder above them; a folded run of entries carries
// its members' marks. prefix is the listing root relative to the
// repository root. The marks count as shown only once they're printed.
func markTree(root *node, c *workCtx, prefix string) {
	if c == nil || c.st.Changes == nil {
		return
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
		if n.marks = c.fileMarks(f); len(n.marks) > 0 {
			if s := n.inSeries; s != nil && len(s.marks) == 0 {
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
}

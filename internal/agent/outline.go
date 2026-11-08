package agent

import (
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Vaspyyy/prism/outline"
	"github.com/Vaspyyy/prism/ref"
	"github.com/Vaspyyy/prism/repo"
	"github.com/Vaspyyy/prism/report"
)

// Symbol is a definition found by the outline rules.
type Symbol = outline.Symbol

// wholeFile is how many symbols are read when a file's whole outline is
// needed (ordinals, the single-file view): more than real source files have.
const wholeFile = 5000

// HasOutline reports whether the file type has outline rules.
func HasOutline(name string) bool { return outline.Supported(name) }

// Outline returns the symbols of a source file (at most limit).
func Outline(path string, limit int) []Symbol {
	st, err := os.Stat(path)
	if err != nil || st.Size() > outline.MaxSize {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return outline.Parse(path, data, limit)
}

// outlineRefs returns the first limit symbols of a source file and the name
// a ref gives each (see refNames). Ordinals count the whole file, so a
// name reads the same here as in the single-file view.
func outlineRefs(path string, limit int) ([]Symbol, []string) {
	syms := Outline(path, wholeFile)
	names := refNames(syms)
	if len(syms) > limit {
		syms, names = syms[:limit], names[:limit]
	}
	return syms, names
}

// refNames returns the name a ref gives each of a file's symbols: its
// qualified name ("globSet.match", "outer.inner"), plus "#N" when the file
// defines that name more than once. Pass the file's whole outline.
func refNames(syms []Symbol) []string {
	refs := ref.Refs(ref.Targets("", syms))
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.String() // no path: just the name and its ordinal
	}
	return names
}

// symbolLine is a symbol's line in the single-file view: its signature,
// after its ref name when the signature doesn't show that name as is
// ("globSet.match  func (g globSet) match(p string) bool"), so the header
// path + ":" + the line's name is always a ref.
func symbolLine(s Symbol, name string) string {
	if showsName(s.Sig, name) {
		return s.Sig
	}
	return name + "  " + s.Sig
}

// showsName reports whether sig contains name as a whole word: not glued to
// a letter, digit, '_', '$' or '.' on either side, so "toSlug" is not shown
// as is in "fun String.toSlug()".
func showsName(sig, name string) bool {
	if name == "" {
		return false
	}
	first, size := utf8.DecodeRuneInString(name)
	last, _ := utf8.DecodeLastRuneInString(name)
	for i := 0; i < len(sig); {
		j := strings.Index(sig[i:], name)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(name)
		before, _ := utf8.DecodeLastRuneInString(sig[:start])
		after, _ := utf8.DecodeRuneInString(sig[end:])
		if (start == 0 || !glued(first) || !glued(before)) && (end == len(sig) || !glued(last) || !glued(after)) {
			return true
		}
		i = start + size
	}
	return false
}

func glued(r rune) bool {
	return r == '_' || r == '$' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// WorkRepo starts looking for the repository containing the working
// directory, in the background since it runs git, and returns a function
// that waits for it. Outside a repository the Info has Git false.
func WorkRepo() func() *repo.Info {
	ch := make(chan *repo.Info, 1)
	go func() {
		in, err := repo.Detect(".")
		if err != nil {
			in = &repo.Info{}
		}
		ch <- in
	}()
	return sync.OnceValue(func() *repo.Info { return <-ch })
}

// headerPath names a file in the single-file view's header: relative to the
// root of the working directory's repository when the file is inside it,
// the form refs use, else absolute. here is nil with --no-git.
func headerPath(abs string, here func() *repo.Info) string {
	if here == nil {
		return abs
	}
	if in := here(); in.Git {
		if rel, ok := in.Rel(abs); ok {
			return rel
		}
	}
	return abs
}

// File is the single-file view as data, for --json: the path as the header
// names it, the line count and size, and the whole outline with each
// symbol's ref (the same names the text view prints). here is as for the
// header, nil with --no-git. At most limit symbols are returned; truncated
// says when there were more.
func File(path string, st fs.FileInfo, opt Options, here func() *repo.Info, limit int) (f report.GlintFile, truncated *report.Truncated) {
	opt.defaults()
	opt.NoGit = true // git status and history aren't part of the result
	abs, _ := filepath.Abs(path)
	n := fileNode(abs, st, opt)
	f = report.GlintFile{Path: headerPath(abs, here), Lines: n.lines, Size: n.size, Symbols: []report.Symbol{}}
	if len(n.symbols) == 0 { // like the text view: no outline for binary, huge or symlinked files
		return f, nil
	}
	// the whole file, so ordinals count every definition (files are at most outline.MaxSize)
	syms := report.Symbols(f.Path, Outline(abs, math.MaxInt))
	if len(syms) > limit {
		truncated = &report.Truncated{Shown: limit, Total: len(syms)}
		syms = syms[:limit]
	}
	f.Symbols = syms
	return f, truncated
}

package agent

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// The other tests check output that depends only on the arguments and the
// files: context (marks, the context: line) stays off unless a test turns
// it on with workRepo.
func TestMain(m *testing.M) {
	os.Setenv("PRISM_NO_CONTEXT", "1")
	os.Exit(m.Run())
}

// isolateGit turns context on and keeps the user's git config and agent
// session out of the test.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(k, v)
	}
	for _, k := range []string{"PRISM_NO_CONTEXT", "PRISM_SESSION", "CLAUDE_CODE_SESSION_ID", "T3CODE_THREAD_ID", "GIT_DIR", "GIT_WORK_TREE", "GIT_OPTIONAL_LOCKS"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// workRepo makes a repository on branch feature, split off main:
//
//   - internal/render/render.go: Draw changed in a commit on the branch
//   - internal/files/files.go: the second init changed, not committed
//   - frames/frame_00N.txt: a run of files, one of them changed on the branch
//   - new.go: untracked
func workRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	files := "package files\n\nfunc List() []string {\n\treturn nil\n}\n\nfunc init() {}\n\nfunc init() {}\n"
	write(t, filepath.Join(dir, "go.mod"), "module example.com/demo\n\ngo 1.22\n")
	write(t, filepath.Join(dir, "internal", "files", "files.go"), files)
	write(t, filepath.Join(dir, "internal", "render", "render.go"), "package render\n\nfunc Draw() {}\n\nfunc Clear() {}\n")
	write(t, filepath.Join(dir, "cmd", "main.go"), "package main\n\nfunc main() {}\n")
	for i := 1; i <= 4; i++ {
		write(t, filepath.Join(dir, "frames", fmt.Sprintf("frame_%03d.txt", i)), "x\n")
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "init")
	gitIn(t, dir, "checkout", "-qb", "feature")
	write(t, filepath.Join(dir, "internal", "render", "render.go"), "package render\n\nfunc Draw() {\n\tprintln(1)\n}\n\nfunc Clear() {}\n")
	write(t, filepath.Join(dir, "frames", "frame_002.txt"), "y\n")
	gitIn(t, dir, "commit", "-qam", "draw")
	i := strings.LastIndex(files, "func init() {}")
	write(t, filepath.Join(dir, "internal", "files", "files.go"), files[:i]+"func init() { println(2) }\n")
	write(t, filepath.Join(dir, "new.go"), "package demo\n\nfunc New() {}\n")
	return dir
}

func agentView(t *testing.T, opt Options, paths ...string) string {
	t.Helper()
	var buf bytes.Buffer
	Run(&buf, paths, opt)
	return buf.String()
}

// unmark is context-on output without the context: line and the marks:
// what --no-context prints.
var markRe = regexp.MustCompile(` \[(changed|branch|\d+ branch)\]`)

func unmark(out string) string {
	if strings.HasPrefix(out, "context: ") {
		_, out, _ = strings.Cut(out, "\n")
	}
	return markRe.ReplaceAllString(out, "")
}

func contains(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
}

func TestContextMarksMap(t *testing.T) {
	dir := workRepo(t)
	// a map without an outline loads file marks only, for what it lists
	for _, c := range []struct{ name, cwd, path, line string }{
		{"root", dir, ".", "context: 4 changed files\n"},
		{"subdirectory", filepath.Join(dir, "internal"), ".", "context: 2 changed files\n"},
		{"subdirectory, given the root", filepath.Join(dir, "internal"), "..", "context: 4 changed files\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Chdir(c.cwd)
			out := agentView(t, Options{}, c.path)
			if !strings.HasPrefix(out, c.line) {
				t.Errorf("want the context: line first, got:\n%s", out)
			}
			// uncommitted: the git status says it, so no [changed] next to it
			contains(t, out, "render.go 7L [branch]\n", "files.go 9L [M]\n")
			if !regexp.MustCompile(`\n *render/ \([^)]*\) \[1 branch\]\n`).MatchString(out) {
				t.Errorf("want the folder to count its file changed on the branch:\n%s", out)
			}
			if strings.Contains(out, "[changed]") {
				t.Errorf("a file mark duplicates the git status:\n%s", out)
			}
			if c.path != "." || c.cwd == dir {
				contains(t, out, "frame_{001..004}.txt ×4 1L each [branch]\n", "frames/ (4 files, 8B) [1 branch]\n")
			}
		})
	}

	t.Chdir(dir)
	out := agentView(t, Options{Outline: true}, ".")
	contains(t, out, "↳ List:3 init#1:7 init#2:9 [changed]\n", "↳ Draw:3 [branch] Clear:7\n")
	// new.go's symbol is a changed one too; the frames have no outline
	if !strings.HasPrefix(out, "context: 2 changed symbols vs HEAD, 1 symbol changed on this branch since ") {
		t.Errorf("want the context: line first, got:\n%s", out)
	}
	contains(t, out, "new.go 3L [new]\n  ↳ New:3\n") // every symbol of a new file is new: not marked

	// off: the same output minus the context: line and the marks
	off := agentView(t, Options{Outline: true, NoContext: true}, ".")
	if got := unmark(out); got != off {
		t.Errorf("--no-context should only drop the marks and the context: line\ncontext on, unmarked:\n%s\noff:\n%s", got, off)
	}
	t.Setenv("PRISM_NO_CONTEXT", "1")
	if got := agentView(t, Options{Outline: true}, "."); got != off {
		t.Errorf("PRISM_NO_CONTEXT=1:\n%s\nwant:\n%s", got, off)
	}
	t.Setenv("PRISM_NO_CONTEXT", "0") // "0" means on
	if got := agentView(t, Options{Outline: true}, "."); got != out {
		t.Errorf("PRISM_NO_CONTEXT=0:\n%s\nwant:\n%s", got, out)
	}
	if got := agentView(t, Options{Outline: true, NoGit: true}, "."); strings.Contains(got, "context:") || markRe.MatchString(got) {
		t.Errorf("--no-git runs no git, so it has no context:\n%s", got)
	}
}

func TestContextMarksOutline(t *testing.T) {
	dir := workRepo(t)
	t.Chdir(filepath.Join(dir, "internal"))
	out := agentView(t, Options{}, filepath.Join("files", "files.go"))
	// the header keeps its git status; the changed symbol is marked
	header, body, _ := strings.Cut(unmarkLine(out), "\n")
	want := "  3 func List() []string\n" +
		"  7 init#1  func init()\n" +
		"  9 init#2  func init() [changed]\n"
	if !strings.HasPrefix(header, "internal/files/files.go ") || !strings.HasSuffix(header, " 9L (last commit 0m ago: init) [M], modified 0m ago") || body != want {
		t.Errorf("got\n%s\nwant the header, then\n%s", out, want)
	}
	if !strings.HasPrefix(out, "context: 1 changed symbol vs HEAD\n") { // only the file's own changes

		t.Errorf("want the context: line first:\n%s", out)
	}

	out = agentView(t, Options{}, filepath.Join("render", "render.go"))
	contains(t, out, ") [branch], modified ", "  3 func Draw() [branch]\n", "  7 func Clear()\n")

	// a new file's git status says every symbol in it is new
	out = agentView(t, Options{}, filepath.Join("..", "new.go"))
	if strings.Contains(out, "[changed]") || strings.Contains(out, "context:") {
		t.Errorf("a new file's symbols are not marked:\n%s", out)
	}

	off := agentView(t, Options{NoContext: true}, filepath.Join("files", "files.go"), filepath.Join("render", "render.go"))
	on := agentView(t, Options{}, filepath.Join("files", "files.go"), filepath.Join("render", "render.go"))
	if strings.Count(on, "context: ") != 1 {
		t.Errorf("two files of one repository share one context: line:\n%s", on)
	}
	if got := unmark(on); got != off {
		t.Errorf("--no-context should only drop the marks and the context: line\non, unmarked:\n%s\noff:\n%s", got, off)
	}
}

// unmarkLine drops the context: line only.
func unmarkLine(out string) string {
	if strings.HasPrefix(out, "context: ") {
		_, out, _ = strings.Cut(out, "\n")
	}
	return out
}

// The context: line counts against the budget: a map that fills it gives
// back a glance line instead.
// gridRepo makes a repository on main whose map with --budget 24 fills
// its budget: 12 folders of 4 folders of 3 Go files each, committed.
func gridRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	words := strings.Fields("alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima")
	for _, d := range words {
		for _, sub := range words[:4] {
			for _, f := range []string{"a.go", "b.go", "c.go"} {
				write(t, filepath.Join(dir, d, sub+"x", f), "package a\n\nfunc F() {}\n")
			}
		}
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "init")
	t.Chdir(dir)
	return dir
}

// The context: line counts against the budget: a map that fills it gives
// back a glance line instead.
func TestContextLineCountsAgainstBudget(t *testing.T) {
	dir := gridRepo(t)
	gitIn(t, dir, "checkout", "-qb", "feature")
	write(t, filepath.Join(dir, "alpha", "alphax", "a.go"), "package a\n\nfunc A() {}\n")
	gitIn(t, dir, "commit", "-qam", "A")

	on, off := agentView(t, Options{Budget: 44}, "."), agentView(t, Options{Budget: 44, NoContext: true}, ".")
	if !strings.HasPrefix(on, "context: ") || !strings.Contains(off, "↳ ") {
		t.Fatalf("want a context: line, and glances without it:\non:\n%s\noff:\n%s", on, off)
	}
	if n, m := strings.Count(on, "\n"), strings.Count(off, "\n"); n != m {
		t.Errorf("context on: %d lines, off: %d\non:\n%s\noff:\n%s", n, m, on, off)
	}
	if strings.Count(on, "↳ ") != strings.Count(off, "↳ ")-1 {
		t.Errorf("want one glance fewer with the context: line:\non:\n%s\noff:\n%s", on, off)
	}
}

// In --changed a glance lists the changed files of a collapsed folder:
// entries, never given up for the context: line, which then goes on top.
func TestContextLineNeverCostsEntries(t *testing.T) {
	dir := gridRepo(t)
	write(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	gitIn(t, dir, "add", "main.go")
	gitIn(t, dir, "commit", "-qm", "main")
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".go") {
			write(t, p, "package a\n\nfunc F() { println(1) }\n")
		}
		if d != nil && d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	opt := Options{Budget: 24, Changed: true, Outline: true}
	on := agentView(t, opt, ".")
	opt.NoContext = true
	off := agentView(t, opt, ".")
	if !strings.HasPrefix(on, "context: ") || !strings.Contains(on, "main.go 3L [M]\n  ↳ F:3 [changed]\n") || !strings.Contains(off, "[M], ") {
		t.Fatalf("want a context: line and glances listing changed files:\non:\n%s\noff:\n%s", on, off)
	}
	if got := unmark(on); got != off {
		t.Errorf("context cost entries:\non, unmarked:\n%s\noff:\n%s", got, off)
	}
}

// The line is fitted into the budget only when the map prints a mark: a
// change that shows nowhere costs nothing.
func TestContextLineOnlyForPrintedMarks(t *testing.T) {
	dir := gridRepo(t)
	// under a folder that stays collapsed, and outside any symbol
	write(t, filepath.Join(dir, "lima", "alphax", "a.go"), "// a comment\npackage a\n\nfunc F() {}\n")
	on, off := agentView(t, Options{Budget: 24, Outline: true}, "."), agentView(t, Options{Budget: 24, Outline: true, NoContext: true}, ".")
	if on != off {
		t.Errorf("nothing marked, so nothing should change:\non:\n%s\noff:\n%s", on, off)
	}
}

// A file named in the top level's "… also:" list carries its mark there;
// one that isn't printed doesn't count.
func TestContextMarksOverflowList(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	for i := range 120 { // no digits: not folded into one run
		write(t, filepath.Join(dir, string(rune('a'+i/26))+string(rune('a'+i%26))+".md"), "# t\n")
	}
	write(t, filepath.Join(dir, "goz.md"), "# goz\n")
	write(t, filepath.Join(dir, "pkg", "p.go"), "package p\n")
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "init")
	gitIn(t, dir, "checkout", "-qb", "feature")
	write(t, filepath.Join(dir, "goz.md"), "# goz\n\nmore\n")
	gitIn(t, dir, "commit", "-qam", "goz")
	old := time.Now().Add(-48 * time.Hour) // the oldest file: past the 100 entries listed in full
	if err := os.Chtimes(filepath.Join(dir, "goz.md"), old, old); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out := agentView(t, Options{}, ".")
	also := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "… also: ") {
			also = l
		}
	}
	if !strings.Contains(also, " goz.md [branch]") || !strings.HasPrefix(out, "context: 1 changed file\n") {
		t.Errorf("want goz.md marked in the also: line:\n%s", out)
	}
}

// A file listed next to a big folder keeps its symbol detail: files and
// folders load separately.
func TestContextFileNextToBigFolder(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "init")
	for i := range 205 {
		write(t, filepath.Join(dir, "data", fmt.Sprintf("d%03d.txt", i)), "x\n")
	}
	write(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() { println(1) }\n")
	t.Chdir(dir)
	out := agentView(t, Options{}, "data", "main.go")
	if !strings.Contains(out, "  3 func main() [changed]\n") || strings.Contains(out, "skipped") || !strings.HasPrefix(out, "context: 1 changed symbol vs HEAD\n") {
		t.Errorf("want main marked:\n%s", out)
	}
}

// Files that are new at their path by their git status (added with
// git add -N, renamed) don't get every symbol marked [changed].
func TestContextNewByGitStatus(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "files.go"), "package files\n\nfunc A() {}\n\nfunc B() {}\n")
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "init")
	write(t, filepath.Join(dir, "ita.go"), "package files\n\nfunc C() {}\n")
	gitIn(t, dir, "add", "-N", "ita.go")
	gitIn(t, dir, "mv", "files.go", "moved.go")
	t.Chdir(dir)
	for _, c := range []struct{ file, status string }{{"ita.go", "[A]"}, {"moved.go", "[R]"}} {
		out := agentView(t, Options{}, c.file)
		if !strings.Contains(out, c.status) || strings.Contains(out, "[changed]") {
			t.Errorf("%s: want %s and no symbol marks:\n%s", c.file, c.status, out)
		}
		if out := agentView(t, Options{Outline: true}, "."); strings.Contains(out, "[changed]") {
			t.Errorf("map: want no symbol marks:\n%s", out)
		}
	}
}

// One context: line for the run; with several repositories, each part
// starts with the repository's folder name.
func TestContextOneLineForSeveralRepos(t *testing.T) {
	isolateGit(t)
	parent := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(parent, name)
		write(t, filepath.Join(dir, "x.go"), "package x\n\nfunc X() {}\n")
		gitIn(t, dir, "init", "-q", "-b", "main")
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-qm", "init")
		write(t, filepath.Join(dir, "x.go"), "package x\n\nfunc X() { println(1) }\n")
	}
	t.Chdir(parent)
	out := agentView(t, Options{}, filepath.Join("alpha", "x.go"), filepath.Join("beta", "x.go"))
	if strings.Count(out, "context:") != 1 || !strings.HasPrefix(out, "context: alpha: 1 changed symbol vs HEAD; beta: 1 changed symbol vs HEAD\n") {
		t.Errorf("want one context: line naming both repositories:\n%s", out)
	}
	if out := agentView(t, Options{}, filepath.Join("alpha", "x.go")); !strings.HasPrefix(out, "context: 1 changed symbol vs HEAD\n") {
		t.Errorf("one repository: no name:\n%s", out)
	}
}

// An untracked symlink to /dev/zero doesn't hang loading the context.
func TestContextSymlinkToDevZero(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("no /dev/zero")
	}
	dir := gridRepo(t)
	if err := os.Symlink("/dev/zero", filepath.Join(dir, "zero.go")); err != nil {
		t.Skip(err)
	}
	done := make(chan string, 1)
	go func() { done <- agentView(t, Options{Outline: true}, ".") }()
	select {
	case out := <-done:
		if !strings.Contains(out, "zero.go -> /dev/zero") {
			t.Errorf("want the symlink listed:\n%s", out)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("glint -A --outline hangs on an untracked symlink to /dev/zero")
	}
}

// workspaceDir makes a folder outside git with three repositories, each its
// own project: alpha (workRepo), recently active, so a map of the folder
// opens it up; beta, changed the same way but untouched for a month, and
// gamma, clean and as old: a line each.
func workspaceDir(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.Rename(workRepo(t), filepath.Join(ws, name)); err != nil {
			t.Fatal(err)
		}
	}
	gamma := filepath.Join(ws, "gamma")
	write(t, filepath.Join(gamma, "go.mod"), "module example.com/gamma\n\ngo 1.22\n")
	write(t, filepath.Join(gamma, "x.go"), "package x\n\nfunc X() {}\n")
	gitIn(t, gamma, "init", "-q", "-b", "main")
	gitIn(t, gamma, "add", ".")
	gitIn(t, gamma, "commit", "-qm", "init")
	old := time.Now().Add(-30 * 24 * time.Hour)
	for _, name := range []string{"beta", "gamma"} {
		err := filepath.WalkDir(filepath.Join(ws, name), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.Chtimes(p, old, old)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

// A map of a folder outside git marks the projects it opens up, each with
// its own context, from wherever it's run; the projects it leaves closed
// are a line each, unmarked.
func TestContextWorkspace(t *testing.T) {
	ws := workspaceDir(t)
	outside := t.TempDir()
	for _, c := range []struct{ name, cwd, path string }{
		{"inside", ws, "."},
		{"inside the opened project", filepath.Join(ws, "alpha"), ".."},
		{"inside a closed project", filepath.Join(ws, "beta"), ".."},
		{"outside", outside, ws},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Chdir(c.cwd)
			out := agentView(t, Options{}, c.path)
			if !strings.HasPrefix(out, "context: alpha: 4 changed files\n") {
				t.Errorf("want the context: line first, naming the project:\n%s", out)
			}
			contains(t, out, "; opened: recently active) [2 branch]\n", "    render.go 7L [branch]\n", "    files.go 9L [M]\n",
				"    frame_{001..004}.txt ×4 1L each [branch]\n")
			if !regexp.MustCompile(`\n  internal/ \([^)]*\) \[1 changed\] \[1 branch\]\n`).MatchString(out) {
				t.Errorf("want the folder to count its file changed on the branch:\n%s", out)
			}
			if !regexp.MustCompile(`\nbeta/ \([^)\[]*\)\n  ↳ [^\[]*\n`).MatchString(out) {
				t.Errorf("want beta closed and unmarked:\n%s", out)
			}
			if got, off := unmark(out), agentView(t, Options{NoContext: true}, c.path); got != off {
				t.Errorf("--no-context should only drop the marks and the context: line\ncontext on, unmarked:\n%s\noff:\n%s", got, off)
			}
		})
	}

	// with --outline the symbols it shows are marked too
	t.Chdir(ws)
	out := agentView(t, Options{Outline: true}, ".")
	if !strings.HasPrefix(out, "context: alpha: 4 changed files; 2 changed symbols vs HEAD, 1 symbol changed on this branch since ") {
		t.Errorf("want the context: line first:\n%s", out)
	}
	contains(t, out, "↳ List:3 init#1:7 init#2:9 [changed]\n", "↳ Draw:3 [branch] Clear:7\n", "  new.go 3L [new]\n    ↳ New:3\n")
	if got, off := unmark(out), agentView(t, Options{Outline: true, NoContext: true}, "."); got != off {
		t.Errorf("--no-context should only drop the marks and the context: line\ncontext on, unmarked:\n%s\noff:\n%s", got, off)
	}
	t.Setenv("PRISM_NO_CONTEXT", "1")
	if got := agentView(t, Options{}, "."); strings.Contains(got, "context:") || markRe.MatchString(got) {
		t.Errorf("PRISM_NO_CONTEXT=1 marks nothing:\n%s", got)
	}
}

// In a workspace map the context: line takes a line of the budget that's
// still free, and only when there's none, the last glance: never an entry.
func TestContextWorkspaceBudget(t *testing.T) {
	ws := workspaceDir(t)
	grid := gridRepo(t)
	gitIn(t, grid, "checkout", "-qb", "feature")
	write(t, filepath.Join(grid, "alpha", "alphax", "a.go"), "package a\n\nfunc A() {}\n")
	gitIn(t, grid, "commit", "-qam", "A")
	if err := os.RemoveAll(filepath.Join(ws, "alpha")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(grid, filepath.Join(ws, "alpha")); err != nil {
		t.Fatal(err)
	}
	// old loose folders too big to open from up here: each gets a glance
	old := time.Now().Add(-30 * 24 * time.Hour)
	for _, d := range []string{"misc", "notes", "tmp"} {
		for _, sub := range strings.Split("abcdefghij", "") {
			for _, f := range []string{"a.md", "a.txt"} {
				p := filepath.Join(ws, d, sub, f)
				write(t, p, "a\n")
				if err := os.Chtimes(p, old, old); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Chdir(ws)
	glance := func(l string) bool { return strings.HasPrefix(strings.TrimLeft(l, " "), "↳ ") }
	fits, full := 0, 0
	sweep := func() {
		for budget := 16; budget <= 88; budget += 3 {
			on, off := agentView(t, Options{Budget: budget}, "."), agentView(t, Options{Budget: budget, NoContext: true}, ".")
			if !strings.HasPrefix(on, "context: alpha: 1 changed file\n") {
				continue
			}
			n, m := strings.Count(on, "\n"), strings.Count(off, "\n")
			if m < budget { // a line is free: it takes that, and nothing gives way
				fits++
				if unmark(on) != off {
					t.Errorf("budget %d: the line fits, so nothing should give way\non:\n%s\noff:\n%s", budget, on, off)
				}
				continue
			}
			full++
			entries := slices.DeleteFunc(strings.Split(off, "\n"), glance)
			kept := slices.DeleteFunc(strings.Split(unmark(on), "\n"), glance)
			if n != m || strings.Count(on, "↳ ") != strings.Count(off, "↳ ")-1 || !slices.Equal(kept, entries) {
				t.Errorf("budget %d: the map is full: want the line in place of a glance, every entry kept\non:\n%s\noff:\n%s", budget, on, off)
			}
		}
	}
	sweep()
	// empty folders add lines the budget didn't plan for, so the map fills it
	for _, e := range []string{"empty1", "empty2", "empty3"} {
		if err := os.Mkdir(filepath.Join(ws, "alpha", e), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sweep()
	if fits == 0 || full == 0 {
		t.Errorf("want maps with a free line (got %d) and full ones (got %d)", fits, full)
	}
}

// Projects of the same name in two workspace maps of one run are told apart
// in the context: line.
func TestContextSameNameProjects(t *testing.T) {
	ws1, ws2 := workspaceDir(t), workspaceDir(t)
	out := agentView(t, Options{}, ws1, ws2)
	want := "context: " + filepath.Base(ws1) + "/alpha: 4 changed files; " + filepath.Base(ws2) + "/alpha: 4 changed files\n"
	if !strings.HasPrefix(out, want) {
		t.Errorf("want the context: line\n%sgot:\n%s", want, out)
	}
}

func TestShortNames(t *testing.T) {
	sep := string(filepath.Separator)
	p := func(parts ...string) string { return sep + strings.Join(parts, sep) }
	roots := []string{p("a", "ws1", "alpha"), p("b", "ws1", "alpha"), p("c", "beta"), p("d", "alpha"), p("glint")}
	got := shortNames(roots)
	want := map[string]string{
		roots[0]: strings.Join([]string{"a", "ws1", "alpha"}, sep),
		roots[1]: strings.Join([]string{"b", "ws1", "alpha"}, sep),
		roots[2]: "beta",
		roots[3]: "d" + sep + "alpha",
		roots[4]: "glint",
	}
	if !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// staleIndex makes the index of the repository in dir stale (a clean file's
// stat data changed), so a git status that may take the index lock writes
// it back, and returns a check that it wasn't written.
func staleIndex(t *testing.T, dir, file string) func() {
	t.Helper()
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, file), past, past); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(dir, ".git", "index")
	read := func() ([]byte, time.Time) {
		data, err := os.ReadFile(index)
		info, err2 := os.Stat(index)
		if err != nil || err2 != nil {
			t.Fatal(err, err2)
		}
		return data, info.ModTime()
	}
	data, mtime := read()
	return func() {
		t.Helper()
		if d, m := read(); !bytes.Equal(d, data) || !m.Equal(mtime) {
			t.Errorf("the index of %s was written", dir)
		}
	}
}

// A project's symbol load whose files all turn out to be outside it loads
// nothing: in particular not the whole repository, with git's optional
// locks on.
func TestContextNestedPathsOutside(t *testing.T) {
	dir := workRepo(t)
	unwritten := staleIndex(t, dir, filepath.Join("cmd", "main.go"))
	files, syms := newContexts(false, true).nested(dir, []string{filepath.Join(t.TempDir(), "x.go")})
	if files == nil || syms == nil {
		t.Fatal("want both loads")
	}
	if c := files(); c.st.Changes == nil || len(c.st.Changes.Files) != 4 {
		t.Errorf("want the project's file marks")
	}
	if c := syms(); c.st.Changes != nil {
		t.Errorf("want nothing loaded, got %d changed files", len(c.st.Changes.Files))
	}
	unwritten()
}

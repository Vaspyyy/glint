package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
	for _, k := range []string{"PRISM_NO_CONTEXT", "PRISM_SESSION", "CLAUDE_CODE_SESSION_ID", "T3CODE_THREAD_ID", "GIT_DIR", "GIT_WORK_TREE"} {
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
func TestContextLineCountsAgainstBudget(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	words := strings.Fields("alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima")
	for _, d := range words {
		for _, sub := range words[:4] {
			for _, f := range []string{"a.go", "b.go", "c.go"} {
				write(t, filepath.Join(dir, d, sub+"x", f), "package a\n")
			}
		}
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "init")
	gitIn(t, dir, "checkout", "-qb", "feature")
	write(t, filepath.Join(dir, "alpha", "alphax", "a.go"), "package a\n\nfunc A() {}\n")
	gitIn(t, dir, "commit", "-qam", "A")
	t.Chdir(dir)

	on, off := agentView(t, Options{Budget: 24}, "."), agentView(t, Options{Budget: 24, NoContext: true}, ".")
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

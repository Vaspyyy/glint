package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Vaspyyy/prism/report"
)

// The other tests check output that depends only on the arguments and the
// files: context stays off unless a test turns it on with contextRepo.
func TestMain(m *testing.M) {
	os.Setenv("PRISM_NO_CONTEXT", "1")
	os.Exit(m.Run())
}

// contextRepo makes a repository on branch feature with Draw changed in a
// commit on the branch and the second init changed but not committed, turns
// context on with the user's git config and agent session kept out, and
// changes into it.
func contextRepo(t *testing.T) string {
	t.Helper()
	isolate(t)
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
	dir := t.TempDir()
	write(t, filepath.Join(dir, "files.go"), "package demo\n\nfunc init() {}\n\nfunc init() {}\n")
	write(t, filepath.Join(dir, "render.go"), "package demo\n\nfunc Draw() {}\n\nfunc Clear() {}\n")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"commit", "-qm", "init"}, {"checkout", "-qb", "feature"}} {
		gitRun(t, dir, args...)
	}
	write(t, filepath.Join(dir, "render.go"), "package demo\n\nfunc Draw() {\n\tprintln(1)\n}\n\nfunc Clear() {}\n")
	gitRun(t, dir, "commit", "-qam", "draw")
	write(t, filepath.Join(dir, "files.go"), "package demo\n\nfunc init() {}\n\nfunc init() { println(2) }\n")
	t.Chdir(dir)
	return dir
}

func marks(f *report.GlintFile) map[string][]string {
	out := map[string][]string{}
	for _, s := range f.Symbols {
		out[s.Ref] = s.Marks
	}
	return out
}

func TestFileJSONContext(t *testing.T) {
	contextRepo(t)

	// the context covers the file shown
	env, _ := runJSON(t, "-A", "--json", "files.go")
	c := env.Context
	if c == nil || !slices.Equal(c.Applied, []string{"mark"}) || c.Changed == nil {
		t.Fatalf("context: %+v", c)
	}
	if ch := c.Changed; ch.Base != "HEAD" || ch.BranchBase == "" || ch.Files != 1 || ch.Uncommitted != 1 || ch.Branch != 0 {
		t.Errorf("context.changed: %+v", ch)
	}
	f := glintResult(t, env).File
	if got := marks(f); !slices.Equal(got["files.go:init#2"], []string{"changed"}) || got["files.go:init#1"] != nil {
		t.Errorf("marks: %v", got)
	}
	if !slices.Equal(f.Marks, []string{"changed"}) {
		t.Errorf("file marks: %v", f.Marks)
	}
	env, _ = runJSON(t, "-A", "--json", "render.go")
	f = glintResult(t, env).File
	if got := marks(f); !slices.Equal(got["render.go:Draw"], []string{"branch"}) || got["render.go:Clear"] != nil {
		t.Errorf("marks: %v", got)
	}
	if ch := env.Context.Changed; ch.Files != 1 || ch.Uncommitted != 0 || ch.Branch != 1 || !slices.Equal(f.Marks, []string{"branch"}) {
		t.Errorf("render.go: context.changed %+v, file marks %v", ch, f.Marks)
	}

	// off: context is null and nothing is marked
	for _, args := range [][]string{{"--no-context"}, {"--no-git"}} {
		env, _ = runJSON(t, append([]string{"-A", "--json"}, append(args, "files.go")...)...)
		if env.Context != nil || len(marks(glintResult(t, env).File)["files.go:init#2"]) > 0 {
			t.Errorf("%v: context %+v, marks %v", args, env.Context, marks(glintResult(t, env).File))
		}
	}
	t.Setenv("PRISM_NO_CONTEXT", "1")
	env, _ = runJSON(t, "-A", "--json", "files.go")
	if env.Context != nil || len(marks(glintResult(t, env).File)["files.go:init#2"]) > 0 {
		t.Errorf("PRISM_NO_CONTEXT=1: context %+v", env.Context)
	}
	t.Setenv("PRISM_NO_CONTEXT", "")

	// a listing doesn't load context
	env, _ = runJSON(t, "--json")
	if env.Context != nil {
		t.Errorf("listing: context %+v", env.Context)
	}
	for _, it := range listing(t, glintResult(t, env)) {
		if len(it.Marks) > 0 {
			t.Errorf("listing: %s has marks %v", it.Path, it.Marks)
		}
	}
}

func TestNoContextFlag(t *testing.T) {
	contextRepo(t)
	on := string(output(t, "-A", "--outline"))
	off := string(output(t, "-A", "--outline", "--no-context"))
	if !strings.HasPrefix(on, "context: 1 changed symbol vs HEAD, 1 symbol changed on this branch since ") ||
		!strings.Contains(on, " [branch]\n  ↳ Draw:3 [branch] Clear:7\n") || !strings.Contains(on, "init#2:5 [changed]") {
		t.Errorf("context on:\n%s", on)
	}
	_, unmarked, _ := strings.Cut(on, "\n")
	for _, m := range []string{" [branch]", " [changed]"} {
		unmarked = strings.ReplaceAll(unmarked, m, "")
	}
	if unmarked != off {
		t.Errorf("--no-context should only drop the marks and the context: line\non, unmarked:\n%s\noff:\n%s", unmarked, off)
	}
}

// logGit puts a git in PATH that logs every git command glint runs, and
// returns a function that runs glint and returns its output and that log.
func logGit(t *testing.T) func(args ...string) (out, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script as git")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	bin, file := t.TempDir(), filepath.Join(t.TempDir(), "git.log")
	write(t, filepath.Join(bin, "git"), "#!/bin/sh\necho \"$*\" >> '"+file+"'\nexec '"+real+"' \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func(args ...string) (string, string) {
		t.Helper()
		os.Remove(file)
		out := string(output(t, args...))
		data, _ := os.ReadFile(file)
		return out, string(data)
	}
}

// loadsChanges reports whether a git log shows prism loading the changes:
// it lists untracked files with git status --untracked-files=all.
func loadsChanges(log string) bool {
	return strings.Contains(log, "--untracked-files=all")
}

// Plain listings never load context: no git runs for it, and nothing is
// marked.
func TestListingsLoadNoContext(t *testing.T) {
	contextRepo(t)
	run := logGit(t)
	for _, args := range [][]string{{"-l"}, {"-1"}, {"-T"}, {"-lg", "--changed"}, {"--json"}, {"-A", "--no-context"}, {"-A", "-M"}} {
		out, log := run(args...)
		if loadsChanges(log) || strings.Contains(log, "merge-base") {
			t.Errorf("glint %q loaded context; git ran:\n%s", args, log)
		}
		if strings.Contains(out, "context:") || strings.Contains(out, "[branch]") {
			t.Errorf("glint %q printed context:\n%s", args, out)
		}
	}
	if _, log := run("-A", "--no-git"); log != "" {
		t.Errorf("glint -A --no-git ran git:\n%s", log)
	}
	// the views that show code structure do load it
	if _, log := run("-A", "."); !loadsChanges(log) {
		t.Errorf("glint -A didn't load context; git ran:\n%s", log)
	}
	t.Setenv("PRISM_NO_CONTEXT", "1")
	if _, log := run("-A", "."); loadsChanges(log) {
		t.Errorf("PRISM_NO_CONTEXT=1 glint -A loaded context; git ran:\n%s", log)
	}
}

// Each view loads only what it shows: git status and the branch's files
// are limited to the listed paths, and a map without an outline loads file
// marks only (no hunks).
func TestContextLoadsWhatItShows(t *testing.T) {
	dir := contextRepo(t)
	write(t, filepath.Join(dir, "sub", "other.go"), "package sub\n\nfunc Other() {}\n")
	gitRun(t, dir, "add", "sub")
	gitRun(t, dir, "commit", "-qm", "sub")
	write(t, filepath.Join(dir, "sub", "other.go"), "package sub\n\nfunc Other() { println(3) }\n")
	run := logGit(t)
	status := func(log, spec string) bool {
		for _, l := range strings.Split(log, "\n") {
			if strings.HasPrefix(l, "status --porcelain -z --no-renames --untracked-files=all") {
				return strings.HasSuffix(l, "--ignore-submodules=dirty"+spec)
			}
		}
		return false
	}
	hunks := func(log string) bool { return strings.Contains(log, "diff -U0") }

	out, log := run("-A", "files.go")
	if !status(log, " -- files.go") || !strings.Contains(log, "diff --name-only --no-renames -z ") || !strings.Contains(out, "  5 init#2  func init() [changed]\n") {
		t.Errorf("a file: git ran:\n%s\noutput:\n%s", log, out)
	}
	if !strings.HasPrefix(out, "context: 1 changed symbol vs HEAD\n") {
		t.Errorf("a file: the context: line counts its own changes:\n%s", out)
	}
	out, log = run("-A")
	if !status(log, "") || hunks(log) || !strings.HasPrefix(out, "context: 3 changed files\n") || !strings.Contains(out, " [branch]\n") {
		t.Errorf("the map: git ran:\n%s\noutput:\n%s", log, out)
	}
	out, log = run("-A", "sub")
	if !status(log, " -- sub") || hunks(log) || strings.HasPrefix(out, "context:") {
		t.Errorf("a folder: git ran:\n%s\noutput:\n%s", log, out)
	}
	out, log = run("-A", "--outline", "sub")
	if !status(log, " -- sub") || !hunks(log) || !strings.Contains(out, "↳ Other:3 [changed]\n") {
		t.Errorf("a folder with --outline: git ran:\n%s\noutput:\n%s", log, out)
	}
	// two paths of one repository share one load and one context: line
	out, log = run("-A", "files.go", "render.go")
	if !status(log, " -- files.go render.go") || strings.Count(out, "context:") != 1 || !strings.HasPrefix(out, "context: 1 changed symbol vs HEAD, 1 symbol changed on this branch since ") {
		t.Errorf("two files: git ran:\n%s\noutput:\n%s", log, out)
	}
}

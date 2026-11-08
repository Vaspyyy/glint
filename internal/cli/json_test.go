package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Vaspyyy/prism/ref"
	"github.com/Vaspyyy/prism/report"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// isolate keeps the user's config file and GLINT_OPTS out of a test.
func isolate(t *testing.T) string {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("GLINT_OPTS", "")
	return cfg
}

// runJSON runs glint and checks that stdout is one valid envelope and
// stderr is empty.
func runJSON(t *testing.T, args ...string) (*report.Decoded, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	status := run(args, &out, &errOut)
	if errOut.Len() > 0 {
		t.Errorf("glint %q wrote to stderr:\n%s", args, errOut.String())
	}
	env, err := report.Check(out.Bytes())
	if err != nil {
		t.Fatalf("glint %q: %v\n%s", args, err, out.String())
	}
	if env.Tool != "glint" || env.Version != version || !slices.Equal(env.Args, args) {
		t.Errorf("glint %q: envelope says tool %q, version %q, args %q", args, env.Tool, env.Version, env.Args)
	}
	checkRefs(t, out.Bytes())
	return env, status
}

// checkRefs checks that every ref in a document reads back as itself.
func checkRefs(t *testing.T, data []byte) {
	t.Helper()
	refs, err := report.Refs(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range refs {
		if got := ref.Parse(s, nil).String(); got != s {
			t.Errorf("ref %q reads back as %q", s, got)
		}
	}
}

func glintResult(t *testing.T, env *report.Decoded) report.Glint {
	t.Helper()
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}
	var g report.Glint
	if err := json.Unmarshal(env.Result, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

type entry struct {
	path     string
	children []entry
}

func entries(items []report.GlintEntry) []entry {
	var out []entry
	for _, it := range items {
		out = append(out, entry{it.Path, entries(it.Children)})
	}
	return out
}

func TestListingJSON(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "b.go"), "package b\n")
	write(t, filepath.Join(dir, "a.txt"), "hello\n")
	write(t, filepath.Join(dir, "sub", "c.txt"), "x\n")
	t.Chdir(dir)

	env, status := runJSON(t, "--json", "--no-git")
	if status != 0 {
		t.Errorf("exit %d", status)
	}
	if env.Repo.Git || !sameDir(t, env.Repo.Root, dir) {
		t.Errorf("--no-git: repo should be the working directory without git, got %+v", env.Repo)
	}
	if env.Truncated != nil {
		t.Errorf("nothing was cut, got truncated %+v", env.Truncated)
	}
	g := glintResult(t, env)
	if g.File != nil {
		t.Errorf("a listing has no file: %+v", g.File)
	}
	var names []string
	for _, it := range g.Items {
		names = append(names, it.Name)
	}
	if !slices.Equal(names, []string{"sub", "a.txt", "b.go"}) {
		t.Fatalf("items %q", names)
	}
	sub, txt := g.Items[0], g.Items[1]
	if sub.Type != "dir" || sub.Path != "sub" || len(sub.Children) != 0 {
		t.Errorf("sub: %+v", sub)
	}
	if txt.Type != "file" || txt.Path != "a.txt" || txt.Size != 6 || txt.Mode != "-rw-r--r--" || txt.Octal != "0644" || txt.Modified.IsZero() {
		t.Errorf("a.txt: %+v", txt)
	}
}

func TestTreeJSON(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "top.txt"), "x\n")
	write(t, filepath.Join(dir, "src", "main.go"), "package main\n")
	write(t, filepath.Join(dir, "src", "lib", "util.go"), "package lib\n")
	t.Chdir(filepath.Dir(dir))

	// paths stay as listed, relative to the working directory like ls
	root := filepath.Base(dir)
	env, status := runJSON(t, "--json", "--no-git", "-T", root)
	if status != 0 {
		t.Errorf("exit %d", status)
	}
	got := entries(glintResult(t, env).Items)
	want := []entry{
		{root + "/src", []entry{
			{root + "/src/lib", []entry{{root + "/src/lib/util.go", nil}}},
			{root + "/src/main.go", nil},
		}},
		{root + "/top.txt", nil},
	}
	if !equalEntries(got, want) {
		t.Errorf("tree:\ngot  %+v\nwant %+v", got, want)
	}

	// -L limits the depth
	env, _ = runJSON(t, "--json", "--no-git", "-T", "-L", "1", root)
	if items := glintResult(t, env).Items; len(items) != 2 || len(items[0].Children) != 0 {
		t.Errorf("-L 1: %+v", entries(items))
	}
}

func equalEntries(a, b []entry) bool {
	return slices.EqualFunc(a, b, func(x, y entry) bool { return x.path == y.path && equalEntries(x.children, y.children) })
}

func TestFileJSON(t *testing.T) {
	isolate(t)
	proj := t.TempDir()
	src := "package files\n\ntype globSet []string\n\nfunc (g globSet) match(p string) bool {\n\treturn true\n}\n\nfunc init() {}\n\nfunc init() {}\n"
	write(t, filepath.Join(proj, "internal", "files", "files.go"), src)
	gitInit(t, proj)
	other := filepath.Join(t.TempDir(), "other.go")
	write(t, other, "package other\n\nfunc Run() {}\n")
	t.Chdir(filepath.Join(proj, "internal")) // a subdirectory: paths stay relative to the root

	env, status := runJSON(t, "-A", "--json", filepath.Join("files", "files.go"))
	if status != 0 {
		t.Errorf("exit %d", status)
	}
	if !env.Repo.Git || !sameDir(t, env.Repo.Root, proj) || env.Repo.Head == "" {
		t.Errorf("repo: %+v", env.Repo)
	}
	g := glintResult(t, env)
	if g.Items != nil || g.File == nil {
		t.Fatalf("want a file, got %+v", g)
	}
	f := g.File
	const path = "internal/files/files.go"
	if f.Path != path || f.Lines != 11 || f.Size != int64(len(src)) {
		t.Errorf("file: path %q, %d lines, %d bytes", f.Path, f.Lines, f.Size)
	}
	want := []report.Symbol{
		{Ref: path + ":globSet", Path: path, Name: "globSet", Kind: "type", Line: 3, End: 3, Sig: "type globSet []string"},
		{Ref: path + ":globSet.match", Path: path, Name: "globSet.match", Kind: "func", Line: 5, End: 7, Sig: "func (g globSet) match(p string) bool"},
		{Ref: path + ":init#1", Path: path, Name: "init", Kind: "func", Line: 9, End: 9, Sig: "func init()"},
		{Ref: path + ":init#2", Path: path, Name: "init", Kind: "func", Line: 11, End: 11, Sig: "func init()"},
	}
	if !slices.EqualFunc(f.Symbols, want, func(a, b report.Symbol) bool {
		return a.Ref == b.Ref && a.Path == b.Path && a.Name == b.Name && a.Kind == b.Kind && a.Line == b.Line && a.End == b.End && a.Sig == b.Sig
	}) {
		t.Errorf("symbols:\ngot  %+v\nwant %+v", f.Symbols, want)
	}

	// outside the repository the path is absolute, and so are the refs
	env, _ = runJSON(t, "-A", "--json", other)
	f = glintResult(t, env).File
	if f.Path != other || len(f.Symbols) != 1 || f.Symbols[0].Ref != other+":Run" {
		t.Errorf("outside the repo: %+v", f)
	}

	// --no-git runs no git: the path is absolute, the repo the working directory
	env, _ = runJSON(t, "-A", "--json", "--no-git", filepath.Join("files", "files.go"))
	abs, _ := filepath.Abs(filepath.Join("files", "files.go"))
	if f := glintResult(t, env).File; f.Path != abs || f.Symbols[0].Ref != abs+":globSet" {
		t.Errorf("--no-git: %+v", f)
	}
	if env.Repo.Git {
		t.Errorf("--no-git: repo %+v", env.Repo)
	}
}

// A file without an outline still gets "symbols": [].
func TestFileJSONWithoutOutline(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"), "one\ntwo")
	t.Chdir(dir)
	var out bytes.Buffer
	if status := run([]string{"-A", "--json", "--no-git", "notes.txt"}, &out, &bytes.Buffer{}); status != 0 {
		t.Errorf("exit %d", status)
	}
	env, err := report.Check(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	f := glintResult(t, env).File
	if f.Lines != 2 || f.Size != 7 || f.Symbols == nil || !bytes.Contains(out.Bytes(), []byte(`"symbols": []`)) {
		t.Errorf("got %+v\n%s", f, out.String())
	}
}

func TestJSONErrors(t *testing.T) {
	cfg := isolate(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.go"), "package a\n")
	write(t, filepath.Join(dir, "b.go"), "package b\n")
	t.Chdir(dir)

	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"--json", "--no-git", "missing"}, report.CodeFailed},
		{[]string{"--json", "--no-git", "a.go", "missing"}, report.CodeFailed}, // no partial listing
		{[]string{"-A", "--json", "--no-git", "missing.go"}, report.CodeFailed},
		{[]string{"--json", "--no-git", "--bogus"}, report.CodeUsage},
		{[]string{"--no-git", "--bogus", "--json"}, report.CodeUsage}, // --json after the bad flag
		{[]string{"--json", "--no-git", "--sort", "colour"}, report.CodeUsage},
		{[]string{"--json", "--no-git", "--since", "soon"}, report.CodeUsage},
		{[]string{"--json", "--no-git", "-P"}, report.CodeUsage},
		{[]string{"--json", "--no-git", "-C"}, report.CodeUsage},
		{[]string{"--json", "--no-git", "-M"}, report.CodeUsage},
		{[]string{"--json", "--no-git", "--thumbs"}, report.CodeUsage},
		{[]string{"-A", "--json", "--no-git", "."}, report.CodeUsage},
		{[]string{"-A", "--json", "--no-git"}, report.CodeUsage}, // the working directory is a directory
		{[]string{"-A", "--json", "--no-git", "a.go", "b.go"}, report.CodeUsage},
		{[]string{"-A", "--json", "--no-git", "-M", "a.go"}, report.CodeUsage},
	} {
		env, status := runJSON(t, c.args...)
		if status != 2 {
			t.Errorf("glint %q: exit %d, want 2", c.args, status)
		}
		if env.Error == nil || env.Error.Code != c.code || env.Error.Message == "" {
			t.Errorf("glint %q: error %+v, want code %q", c.args, env.Error, c.code)
		}
		if env.Repo.Root == "" || env.Repo.Git {
			t.Errorf("glint %q: repo %+v", c.args, env.Repo)
		}
	}

	// a broken config file fails too
	write(t, filepath.Join(cfg, "glint", "config.toml"), "theme = \n")
	env, status := runJSON(t, "--json", "--no-git")
	if status != 2 || env.Error == nil || env.Error.Code != report.CodeFailed {
		t.Errorf("broken config: exit %d, error %+v", status, env.Error)
	}
}

// --interactive and --watch with --json fail like the other views without
// JSON. Checked on listJSON itself, so a test can never open the browser.
func TestJSONRefusesInteractiveViews(t *testing.T) {
	isolate(t)
	t.Chdir(t.TempDir())
	for _, o := range []options{{interactive: true}, {watch: true}} {
		o.json, o.noGit = true, true
		var out bytes.Buffer
		a := &app{o: o, out: &out, errOut: &out, args: []string{"--json"}}
		a.flags = newFlagSet(&options{})
		if status := a.listJSON(); status != 2 {
			t.Errorf("%+v: exit %d", o, status)
		}
		env, err := report.Check(out.Bytes())
		if err != nil || env.Error == nil || env.Error.Code != report.CodeUsage {
			t.Errorf("%+v: %v\n%s", o, err, out.String())
		}
	}
}

func TestWantsJSON(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{[]string{"-l", "--json"}, true},
		{[]string{"--json=true"}, true},
		{[]string{"--json", "--json=false"}, false},
		{[]string{"--", "--json"}, false},
		{[]string{"-l"}, false},
	} {
		if got := wantsJSON(c.args); got != c.want {
			t.Errorf("wantsJSON(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

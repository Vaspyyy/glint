package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func TestOutlineGo(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.go")
	write(t, p, "package x\n\ntype Repo struct{}\n\nfunc (r *Repo) Open() {}\n\nfunc helper() {\n\tif true {}\n}\n")
	got := Outline(p, 10)
	want := []Symbol{
		{Line: 3, End: 3, Kind: "type", Name: "Repo", Sig: "type Repo struct", Parent: -1},
		{Line: 5, End: 5, Kind: "func", Name: "Repo.Open", Sig: "func (r *Repo) Open()", Parent: -1},
		{Line: 7, End: 9, Kind: "func", Name: "helper", Sig: "func helper()", Parent: -1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("symbol %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestOutlineMarkdownSkipsFences(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "README.md")
	write(t, p, "# Title\n\n```sh\n# not a heading\n```\n\n## Usage\n")
	got := Outline(p, 10)
	if len(got) != 2 || got[0].Name != "Title" || got[1].Name != "Usage" || got[1].Kind != "h2" {
		t.Fatalf("got %v", got)
	}
}

func TestListingPrefersCodeOverMedia(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module example.com/demo\n\ngo 1.22\n")
	write(t, filepath.Join(dir, "src", "main.go"), "package main\n\nfunc main() {}\n")
	for i := range 30 {
		write(t, filepath.Join(dir, "assets", "sfx", strings.Repeat("s", i+1)+".ogg"), "\x00\x01")
	}
	write(t, filepath.Join(dir, "node_modules", "left-pad", "index.js"), "module.exports = 1\n")

	var buf bytes.Buffer
	Run(&buf, []string{dir}, Options{NoGit: true})
	out := buf.String()

	for _, want := range []string{
		"Go · example.com/demo · go 1.22", // project detected in the header
		"  main.go 3L",                    // code dir expanded, line counts shown
		"assets/ (30 files",               // media dir summarised...
		"ogg×30",                          // ...by extension
		"node_modules/ (not scanned)",     // junk skipped but mentioned
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\n      sss.ogg") {
		t.Errorf("media files should not get a line each:\n%s", out)
	}
}

func TestTruncationKeepsImportantFiles(t *testing.T) {
	dir := t.TempDir()
	words := strings.Fields(`alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november
		oscar papa quebec romeo sierra tango uniform victor whiskey xray yankee zulu`)
	for i := range 60 {
		write(t, filepath.Join(dir, "sub", words[i%26]+"-"+words[(i/26+3)%26]+"."+[]string{"txt", "cfg"}[i/26%2]), "x\n")
	}
	write(t, filepath.Join(dir, "sub", "README.md"), "# hi\n")
	var buf bytes.Buffer
	Run(&buf, []string{dir}, Options{NoGit: true, PerDir: 10})
	out := buf.String()
	if !strings.Contains(out, "README.md 1L") {
		t.Errorf("README should survive truncation:\n%s", out)
	}
	if !strings.Contains(out, "  … +51 files (") {
		t.Errorf("expected a truncation summary:\n%s", out)
	}
}

func TestSeriesAndCompleteness(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 12; i++ {
		write(t, filepath.Join(dir, "frames", fmt.Sprintf("frame_%03d.txt", i)), "a\nb\n")
	}
	for i := 1; i <= 5; i++ {
		write(t, filepath.Join(dir, fmt.Sprintf("run%d", i), "out.txt"), "x\n")
	}
	os.MkdirAll(filepath.Join(dir, "empty-dir"), 0o755)
	os.MkdirAll(filepath.Join(dir, "only-git", ".git"), 0o755)

	var buf bytes.Buffer
	Run(&buf, []string{dir}, Options{NoGit: true})
	out := buf.String()
	for _, want := range []string{
		"frame_{001..012}.txt ×12 2L each",      // padded, real endpoints
		"run2/ … run5/ (4 more dirs like run1/", // sibling family folded
		"empty-dir/ (empty",
		"only-git/ (no files listed: only .git",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestOutlineC(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.c")
	write(t, p, `#define W 320
/*
 * physical device(s) you want to connect to.
 */
static void kill(Ent *e) { e->dead = 1; }
int main(int argc,
         char **argv)
{
	if (x) { return 0; }
}
// helper(void) in a comment
static inline float lerp(float a, float b, float t)
{
	return a + (b - a) * t;
}
`)
	var names []string
	for _, s := range Outline(p, 50) {
		names = append(names, s.Kind+":"+s.Name)
	}
	got := strings.Join(names, " ")
	if got != "define:W func:kill func:main func:lerp" {
		t.Errorf("got %q", got)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// A project must report the same files and size whether you list it or its parent.
func TestNestedRepoCountsMatch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ws := t.TempDir()
	proj := filepath.Join(ws, "proj")
	write(t, filepath.Join(proj, "main.c"), "int main(void) { return 0; }\n")
	write(t, filepath.Join(proj, ".gitignore"), "backups/\n")
	gitInit(t, proj)
	write(t, filepath.Join(proj, "backups", "big.tar"), strings.Repeat("x", 50000))
	write(t, filepath.Join(proj, "new.txt"), "untracked\n")

	var parent, child bytes.Buffer
	Run(&parent, []string{ws}, Options{})
	Run(&child, []string{proj}, Options{})
	childCounts := strings.SplitN(strings.Split(child.String(), "\n")[1], " (", 2)[0] // "3 files, 81B"
	if !strings.Contains(parent.String(), "proj/ ("+childCounts+";") {
		t.Errorf("parent view disagrees with the project's own header %q:\n%s\n---\n%s", childCounts, parent.String(), child.String())
	}
	if !strings.Contains(parent.String(), "own git repo: ") || !strings.Contains(parent.String(), "1 uncommitted") {
		t.Errorf("nested repo state missing:\n%s", parent.String())
	}
	if strings.Contains(parent.String(), "submodule") {
		t.Errorf("a nested repo is not a submodule:\n%s", parent.String())
	}
}

func TestChangedOutsideGit(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), "x\n")
	var buf bytes.Buffer
	Run(&buf, []string{dir}, Options{Changed: true, NoGit: false})
	if !strings.Contains(buf.String(), "--changed needs git") || strings.Contains(buf.String(), "a.txt") {
		t.Errorf("--changed outside git must refuse, not list everything:\n%s", buf.String())
	}
}

func TestDiskUsageCountsHardlinksOnce(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a", "data.bin"), strings.Repeat("x", 64<<10))
	if err := os.Link(filepath.Join(dir, "a", "data.bin"), filepath.Join(dir, "a", "link.bin")); err != nil {
		t.Skip("hardlinks unsupported")
	}
	var buf bytes.Buffer
	DiskUsage(&buf, []string{dir}, Options{})
	if !strings.Contains(buf.String(), " on disk in 1 file ") {
		t.Errorf("hardlink counted twice:\n%s", buf.String())
	}
}

func TestExtKey(t *testing.T) {
	for name, want := range map[string]string{
		"main.go": "go", "libfoo.so.0": "so", "app.0": "0", "Makefile": "makefile",
		"Opus-5.5@2026-11-3-0325": "other", "DeepSeek-Flash-1017@2026": "(no ext)", "a.tar.gz": "gz",
	} {
		if got := extKey(name); got != want {
			t.Errorf("extKey(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestReadmeSummary(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "README.md"), "<div align=\"center\">\n\n# demo\n\n[![CI](https://x/badge.svg)](https://x)\n\nA **tiny** tool for [counting](https://example.com) things.\nIt is fast.\n\n## Install\n\nmore text\n")
	if got := readmeSummary(dir); got != "A tiny tool for counting things. It is fast." {
		t.Errorf("got %q", got)
	}
	if got := readmeSummary(t.TempDir()); got != "" {
		t.Errorf("no README should give no summary, got %q", got)
	}
}

// outlineLines returns the symbol lines of the single-file view of path.
func outlineLines(t *testing.T, path string) []string {
	t.Helper()
	var buf bytes.Buffer
	Run(&buf, []string{path}, Options{NoGit: true})
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	return lines[1:]
}

// Header path + ":" + the name a symbol line starts with must be a ref:
// the qualified name goes first when the signature doesn't show it as is.
func TestOutlineShowsRefNames(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct{ src, want string }{
		"methods.go": {
			"package x\n\ntype globSet []string\n\nfunc (g globSet) match(p string) bool {\n\treturn true\n}\n\nfunc List() {}\n",
			"  3 type globSet []string\n" +
				"  5 globSet.match  func (g globSet) match(p string) bool\n" +
				"  9 func List()",
		},
		"dups.go": {
			"package x\n\nfunc init() {}\n\nfunc init() {}\n\nfunc run() {}\n",
			"  3 init#1  func init()\n" +
				"  5 init#2  func init()\n" +
				"  7 func run()",
		},
		"nested.py": {
			"def outer():\n    def inner():\n        pass\n    return inner\n\nclass Job:\n    def run(self):\n        pass\n",
			"  1 def outer():\n" +
				"  2 outer.inner  def inner():\n" +
				"  6 class Job:\n" +
				"  7 Job.run  def run(self):",
		},
		"shape.rs": {
			"struct Circle {\n    r: f64,\n}\n\nimpl Circle {\n    fn new() -> Self {\n        Circle { r: 1.0 }\n    }\n}\n\nimpl Shape for Circle {\n    fn area(&self) -> f64 {\n        self.r\n    }\n}\n",
			"  1 struct Circle\n" +
				"  5 Circle#2  impl Circle\n" +
				"  6 Circle.new  fn new() -> Self\n" +
				"  11 impl Shape for Circle\n" +
				"  12 Circle.area  fn area(&self) -> f64",
		},
		"ext.kt": {
			"fun String.toSlug(): String {\n    return this\n}\n",
			"  1 toSlug  fun String.toSlug(): String",
		},
		"README.md": {
			"# seek\n\n## Install\n\n## For coding agents\n",
			"  1 # seek\n" +
				"  3 ## Install\n" +
				"  5 ## For coding agents",
		},
	} {
		p := filepath.Join(dir, name)
		write(t, p, c.src)
		if got := strings.Join(outlineLines(t, p), "\n"); got != c.want {
			t.Errorf("%s:\ngot\n%s\nwant\n%s", name, got, c.want)
		}
	}
}

func TestOutlineDefinesUseRefNames(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.h")
	write(t, p, "#ifdef BIG\n#define W 640\n#else\n#define W 320\n#endif\n#define H 200\n")
	lines := outlineLines(t, p)
	if len(lines) != 1 || lines[0] != "  #defines (3): W#1 W#2 H" {
		t.Errorf("got %q", lines)
	}
}

// The map's inline outline names symbols the same way, ordinals counted
// over the whole file even past the 40 symbols it parses for the map.
func TestMapOutlineUsesRefNames(t *testing.T) {
	dir := t.TempDir()
	var src strings.Builder
	src.WriteString("package x\n\nfunc init() {}\n\ntype T struct{}\n\nfunc (t T) Get() {}\n")
	for i := range 45 {
		fmt.Fprintf(&src, "\nfunc f%d() {}\n", i)
	}
	src.WriteString("\nfunc init() {}\n")
	write(t, filepath.Join(dir, "x.go"), src.String())
	write(t, filepath.Join(dir, "y.py"), "def outer():\n    def inner():\n        pass\n")
	var buf bytes.Buffer
	Run(&buf, []string{dir}, Options{NoGit: true, Outline: true})
	out := buf.String()
	for _, want := range []string{"  ↳ init#1:3 T:5 T.Get:7 f0:9 ", "  ↳ outer:1 outer.inner:2\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestHeaderPathIsRepoRelative(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	proj := t.TempDir()
	write(t, filepath.Join(proj, "internal", "files", "files.go"), "package files\n\nfunc List() {}\n")
	gitInit(t, proj)
	other := filepath.Join(t.TempDir(), "other.go")
	write(t, other, "package other\n")
	t.Chdir(filepath.Join(proj, "internal")) // a subdirectory: paths stay relative to the root

	header := func(path string, opt Options) string {
		var buf bytes.Buffer
		Run(&buf, []string{path}, opt)
		return strings.SplitN(buf.String(), " — ", 2)[0]
	}
	if got := header(filepath.Join("files", "files.go"), Options{}); got != "internal/files/files.go" {
		t.Errorf("inside the repo: got %q", got)
	}
	if got := header(other, Options{}); got != other {
		t.Errorf("outside the repo: got %q, want the absolute path", got)
	}
	abs, _ := filepath.Abs(filepath.Join("files", "files.go"))
	if got := header(filepath.Join("files", "files.go"), Options{NoGit: true}); got != abs {
		t.Errorf("--no-git: got %q, want the absolute path", got)
	}
}

func TestShowsName(t *testing.T) {
	for _, c := range []struct {
		sig, name string
		want      bool
	}{
		{"func List(dir string) error", "List", true},
		{"## For coding agents", "For coding agents", true},
		{"function obj:method()", "obj:method", true},
		{"impl<'a, T> Display for Grid<'a, T>", "Display for Grid<'a, T>", true},
		{"module.exports.run = function ()", "module.exports.run", true},
		{"class Job:", "Job", true},
		{"func Listing() List", "List", true}, // the second occurrence is a whole word
		{"func (g globSet) match(p string) bool", "globSet.match", false},
		{"func Listing()", "List", false},
		{"fun String.toSlug(): String", "toSlug", false},
		{"func init()", "init#2", false},
		{"def inner():", "outer.inner", false},
		{"$el()", "el", false},
	} {
		if got := showsName(c.sig, c.name); got != c.want {
			t.Errorf("showsName(%q, %q) = %v, want %v", c.sig, c.name, got, c.want)
		}
	}
}

// The JSON outline has the same names as the text view, defines included,
// and is cut at the limit it's given.
func TestFileJSONMatchesText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.h")
	write(t, p, "#define W 640\n#define W 320\nstruct Grid { int w; };\nint grid_new(void) { return 0; }\n")
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	f, truncated, _ := File(p, st, Options{}, nil, 100)
	if truncated != nil || f.Path != p || f.Lines != 4 {
		t.Fatalf("got %+v, truncated %+v", f, truncated)
	}
	var names []string
	for _, s := range f.Symbols {
		names = append(names, s.Kind+" "+strings.TrimPrefix(s.Ref, p+":"))
	}
	if got := strings.Join(names, ", "); got != "define W#1, define W#2, struct Grid, func grid_new" {
		t.Errorf("got %s", got)
	}
	text := strings.Join(outlineLines(t, p), "\n")
	if !strings.Contains(text, "#defines (2): W#1 W#2") {
		t.Errorf("text view disagrees:\n%s", text)
	}

	f, truncated, _ = File(p, st, Options{}, nil, 3)
	if len(f.Symbols) != 3 || truncated == nil || truncated.Shown != 3 || truncated.Total != 4 {
		t.Errorf("limit 3: %d symbols, truncated %+v", len(f.Symbols), truncated)
	}
}

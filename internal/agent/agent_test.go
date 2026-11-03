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
	want := []Symbol{{3, "type", "Repo", "type Repo struct"}, {5, "func", "Open", "func (r *Repo) Open()"}, {7, "func", "helper", "func helper()"}}
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

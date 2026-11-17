package agent

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const gib = int64(1) << 30

// flatten turns the nested changes into "depth key delta" lines.
func flatten(cs []*change, depth int) []string {
	var out []string
	for _, c := range cs {
		out = append(out, strings.Repeat(" ", depth)+c.key+" "+signed(c.delta))
		out = append(out, flatten(c.kids, depth+1)...)
	}
	return out
}

func TestDiffSnapsNamesTheFolderThatGrew(t *testing.T) {
	old := map[string]int64{
		".local/": 100 * gib, ".local/share/": 100 * gib, ".local/share/Steam/": 90 * gib,
		".local/share/Steam/common/": 60 * gib, ".local/share/Steam/workshop/": 30 * gib,
		"Trash/": 19 * gib, "proj/": 10 * gib,
	}
	cur := map[string]int64{
		".local/": 149 * gib, ".local/share/": 149 * gib, ".local/share/Steam/": 139 * gib,
		".local/share/Steam/common/": 96 * gib, ".local/share/Steam/common/ETS2/": 36 * gib,
		".local/share/Steam/workshop/": 43 * gib, ".local/share/Steam/workshop/227300/": 13 * gib,
		"Trash/": 3 * gib, "proj/": 10 * gib,
	}
	got := flatten(diffSnaps(old, cur, gib), 0)
	// .local/ and share/ grew only through Steam/, so Steam/ is the line;
	// common/ and workshop/ grew only through one new folder each
	want := []string{
		".local/share/Steam/ +49G",
		" .local/share/Steam/common/ETS2/ +36G",
		" .local/share/Steam/workshop/227300/ +13G",
		"Trash/ -16G",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestDiffSnapsShowsMovesThatCancelOut(t *testing.T) {
	// a folder that barely changed while one child grew and another shrank
	old := map[string]int64{"games/": 50 * gib, "games/a/": 20 * gib, "games/b/": 30 * gib}
	cur := map[string]int64{"games/": 50 * gib, "games/a/": 25 * gib, "games/b/": 25 * gib}
	got := strings.Join(flatten(diffSnaps(old, cur, gib), 0), "\n")
	want := "games/a/ +5.0G\ngames/b/ -5.0G"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func writeRandom(t *testing.T, path string, n int) {
	t.Helper()
	b := make([]byte, n) // random, so a compressing filesystem stores it whole
	rand.Read(b)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiskUsageSaysWhatChangedSinceTheLastRun(t *testing.T) {
	defer func(v int64) { snapMin = v }(snapMin)
	snapMin = 4 << 10
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	writeRandom(t, filepath.Join(dir, "a", "small.bin"), 64<<10)
	writeRandom(t, filepath.Join(dir, "b", "data.bin"), 64<<10)
	run := func(opt Options) string {
		var buf bytes.Buffer
		DiskUsage(&buf, []string{dir}, opt)
		return buf.String()
	}

	if out := run(Options{}); !strings.Contains(out, "disk snapshot saved") {
		t.Fatalf("first run saves a snapshot:\n%s", out)
	}

	writeRandom(t, filepath.Join(dir, "a", "new", "big.bin"), 1<<20)
	os.Remove(filepath.Join(dir, "b", "data.bin"))
	out := run(Options{})
	for _, re := range []string{
		`(?m)^since \d{4}-\d\d-\d\d \d\d:\d\d \(0m ago\): \+`,
		`(?m)^\+\S+ a/new/big\.bin \(new\)$`,
		`(?m)^-\S+ b/data\.bin \(gone\)$`,
	} {
		if !regexp.MustCompile(re).MatchString(out) {
			t.Errorf("second run: no line matching %s in\n%s", re, out)
		}
	}

	// the second run replaced the first snapshot (both within the hour), so
	// a third compares with the second
	if out := run(Options{}); !strings.Contains(out, ": no change of ") {
		t.Errorf("third run should see no change:\n%s", out)
	}
	if n := len(snapFiles(snapRoot(t, dir))); n != 1 {
		t.Errorf("%d snapshots kept, want 1 (runs within an hour replace each other)", n)
	}

	// --since older than any snapshot falls back to the oldest and says so
	out = run(Options{Since: 24 * time.Hour, SinceRaw: "1d"})
	if !strings.Contains(out, "no snapshot is 1d old yet, this is the oldest") {
		t.Errorf("--since 1d:\n%s", out)
	}

	// -I measures something else: no comparison, no snapshot
	if out := run(Options{Ignore: []string{"b"}}); strings.Contains(out, "since ") || strings.Contains(out, "snapshot") {
		t.Errorf("-I should neither compare nor save:\n%s", out)
	}
}

func snapRoot(t *testing.T, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

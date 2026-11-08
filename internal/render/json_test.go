package render

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/prism/report"
)

func dir(name string, kids ...*fsx.Entry) *fsx.Entry {
	return &fsx.Entry{Name: name, Path: name, Mode: fs.ModeDir | 0o755, IsDir: true, Children: kids}
}

func file(name string) *fsx.Entry {
	return &fsx.Entry{Name: name, Path: name, Mode: 0o644, Size: 3}
}

func TestItemsFields(t *testing.T) {
	when := time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC)
	f := file("a.go")
	f.ModTime = when
	f.Commit = &git.Commit{Hash: "b681ef1", Subject: "Add a", Author: "t", Time: when, Count: 4}
	link := &fsx.Entry{Name: "l", Path: "l", Mode: fs.ModeSymlink | 0o777, IsLink: true, Target: "gone", Broken: true}
	d := dir("d")
	d.ChildErr = &fs.PathError{Op: "open", Path: "d", Err: errors.New("permission denied")}
	d.DirSize, d.HasDirSize = 10, true

	items, truncated := Items([]*fsx.Entry{f, link, d}, report.MaxItems)
	if truncated != nil || len(items) != 3 {
		t.Fatalf("got %d items, truncated %+v", len(items), truncated)
	}
	if a := items[0]; a.Type != "file" || a.Kind != "code" || a.Octal != "0644" || !a.Modified.Equal(when) ||
		a.Commit == nil || a.Commit.Hash != "b681ef1" || a.Commit.Count != 4 || a.Children != nil {
		t.Errorf("file: %+v", a)
	}
	if l := items[1]; l.Type != "symlink" || l.Target != "gone" || !l.Broken {
		t.Errorf("symlink: %+v", l)
	}
	if d := items[2]; d.Type != "dir" || d.Error != "permission denied" || d.DirSize == nil || *d.DirSize != 10 {
		t.Errorf("dir: %+v", d)
	}
}

// Every list is cut at the limit; truncated counts entries in the whole tree.
func TestItemsTruncated(t *testing.T) {
	es := []*fsx.Entry{
		dir("a", file("a/1"), file("a/2"), file("a/3")),
		dir("b", file("b/1")),
		file("c"), // cut, with nothing below it
	}
	items, truncated := Items(es, 2)
	if len(items) != 2 || len(items[0].Children) != 2 || len(items[1].Children) != 1 {
		t.Fatalf("got %+v", items)
	}
	// 7 entries in all; shown: a, a/1, a/2, b, b/1
	if truncated == nil || *truncated != (report.Truncated{Shown: 5, Total: 7}) {
		t.Errorf("truncated %+v, want 5 of 7", truncated)
	}
	if _, truncated := Items(es, 3); truncated != nil {
		t.Errorf("nothing cut at 3, got %+v", truncated)
	}
}

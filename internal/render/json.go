package render

import (
	"io/fs"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/prism/report"
)

func fileType(e *fsx.Entry) string {
	m := e.Mode
	switch {
	case m&fs.ModeSymlink != 0:
		return "symlink"
	case m.IsDir():
		return "dir"
	case m&fs.ModeNamedPipe != 0:
		return "pipe"
	case m&fs.ModeSocket != 0:
		return "socket"
	case m&fs.ModeDevice != 0:
		return "device"
	}
	return "file"
}

// Items turns a listing into the items of glint's JSON result (the shape is
// prism's report.GlintEntry), a tree's subdirectories as children. Every
// list is cut at limit entries; truncated is nil unless that dropped some,
// and then counts the entries in the whole tree.
func Items(es []*fsx.Entry, limit int) (items []report.GlintEntry, truncated *report.Truncated) {
	var shown, total int
	items = toItems(es, limit, &shown, &total)
	if shown < total {
		truncated = &report.Truncated{Shown: shown, Total: total}
	}
	return items, truncated
}

func toItems(es []*fsx.Entry, limit int, shown, total *int) []report.GlintEntry {
	out := make([]report.GlintEntry, 0, min(len(es), limit))
	for i, e := range es {
		if i == limit {
			*total += countEntries(es[i:])
			break
		}
		*shown++
		*total++
		_, kind := Lookup(e)
		j := report.GlintEntry{
			Name: e.Name, Path: e.Path, Type: fileType(e), Kind: kind.String(),
			Size: e.Size, Mode: e.Mode.String(), Octal: octal(e.Mode), Modified: e.ModTime,
			Owner: e.Sys.Owner, Group: e.Sys.Group,
			Target: e.Target, Broken: e.Broken, Git: e.Git.String(),
			Children: toItems(e.Children, limit, shown, total),
		}
		if e.HasDirSize {
			j.DirSize = &e.DirSize
		}
		if e.Peek != nil {
			j.Peek = e.Peek.Text
		}
		if c := e.Commit; c != nil {
			j.Commit = &report.GlintCommit{Hash: c.Hash, Subject: c.Subject, Author: c.Author, Time: c.Time, Count: c.Count}
		}
		if e.ChildErr != nil {
			j.Error = ErrMessage(e.ChildErr)
		}
		if len(j.Children) == 0 {
			j.Children = nil
		}
		out = append(out, j)
	}
	return out
}

// countEntries counts entries and everything below them.
func countEntries(es []*fsx.Entry) int {
	n := len(es)
	for _, e := range es {
		n += countEntries(e.Children)
	}
	return n
}

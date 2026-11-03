package render

import (
	"encoding/json"
	"io"
	"io/fs"
	"time"

	"github.com/Vaspyyy/glint/internal/fsx"
)

type jsonEntry struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Type     string      `json:"type"`
	Kind     string      `json:"kind"`
	Size     int64       `json:"size"`
	DirSize  *int64      `json:"dir_size,omitempty"`
	Mode     string      `json:"mode"`
	Octal    string      `json:"octal"`
	Modified time.Time   `json:"modified"`
	Owner    string      `json:"owner,omitempty"`
	Group    string      `json:"group,omitempty"`
	Target   string      `json:"target,omitempty"`
	Broken   bool        `json:"broken,omitempty"`
	Git      string      `json:"git,omitempty"`
	Peek     string      `json:"peek,omitempty"`
	Commit   *jsonCommit `json:"last_commit,omitempty"`
	Error    string      `json:"error,omitempty"`
	Children []jsonEntry `json:"children,omitempty"`
}

type jsonCommit struct {
	Hash    string    `json:"hash"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	Time    time.Time `json:"time"`
	Count   int       `json:"commits"`
}

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

func toJSON(es []*fsx.Entry) []jsonEntry {
	out := make([]jsonEntry, 0, len(es))
	for _, e := range es {
		_, kind := Lookup(e)
		j := jsonEntry{
			Name: e.Name, Path: e.Path, Type: fileType(e), Kind: kind.String(),
			Size: e.Size, Mode: e.Mode.String(), Octal: octal(e.Mode), Modified: e.ModTime,
			Owner: e.Sys.Owner, Group: e.Sys.Group,
			Target: e.Target, Broken: e.Broken, Git: e.Git.String(),
			Children: toJSON(e.Children),
		}
		if e.HasDirSize {
			j.DirSize = &e.DirSize
		}
		if e.Peek != nil {
			j.Peek = e.Peek.Text
		}
		if c := e.Commit; c != nil {
			j.Commit = &jsonCommit{c.Hash, c.Subject, c.Author, c.Time, c.Count}
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

func JSON(w io.Writer, es []*fsx.Entry) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(toJSON(es))
}

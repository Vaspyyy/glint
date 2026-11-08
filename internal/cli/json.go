package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/Vaspyyy/glint/internal/agent"
	"github.com/Vaspyyy/glint/internal/render"
	"github.com/Vaspyyy/prism/repo"
	"github.com/Vaspyyy/prism/report"
)

// With --json glint prints one envelope (prism's report): the listing as
// result.items, or with -A and a file that file's outline as result.file.
// Failures are an envelope too, with the exit code text mode has.

// wantsJSON reports whether args turn on --json. It's for errors found
// before the flags are parsed, or while parsing them.
func wantsJSON(args []string) bool {
	on := false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if v, ok := strings.CutPrefix(arg, "--json="); ok {
			on, _ = strconv.ParseBool(v)
		} else if arg == "--json" {
			on = true
		}
	}
	return on
}

// workDir is the working directory, with symlinks resolved like repo.root:
// what listing paths are relative to.
func workDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(wd); err == nil {
		return real
	}
	return wd
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// workRepo returns the repository containing the working directory, looked
// up once in the background; nil with --no-git, which never runs git.
func (a *app) workRepo() func() *repo.Info {
	if a.o.noGit {
		return nil
	}
	if a.here == nil {
		a.here = agent.WorkRepo()
	}
	return a.here
}

// envelope starts this run's JSON document. Outside git (and with
// --no-git) repo.root is the working directory.
func (a *app) envelope() *report.Envelope {
	var info *repo.Info
	if here := a.workRepo(); here != nil {
		info = here()
	}
	if info == nil || info.Root == "" {
		info = &repo.Info{Root: workDir()}
	}
	return report.New("glint", version, a.args, info)
}

// writeJSON prints env and returns the exit status.
func (a *app) writeJSON(w io.Writer, env *report.Envelope) int {
	if err := report.Write(w, env); err != nil {
		fmt.Fprintf(a.errOut, "glint: cannot write output: %v\n", err)
		return 2
	}
	return 0
}

// failJSON prints an envelope with an error instead of a result and
// returns status, glint's exit code for the failure.
func (a *app) failJSON(w io.Writer, code, message string, status int) int {
	env := a.envelope()
	env.Fail(code, message)
	if a.writeJSON(w, env) != 0 {
		return 2
	}
	return status
}

// listJSON is --json without -A: the listing, in the envelope. Views that
// show more than a listing have nothing to print.
func (a *app) listJSON() int {
	o := &a.o
	for _, v := range []struct {
		on         bool
		flag, hint string
	}{
		{o.interactive, "--interactive", ""},
		{o.watch, "--watch", ""},
		{o.projects, "--projects", ""},
		{o.card, "--card", ""},
		{o.treemap, "--map", "for directory sizes, use glint --json --du"},
		{o.thumbs != "", "--thumbs", ""},
	} {
		if v.on {
			if v.hint == "" {
				v.hint = "run it without --json"
			}
			return a.failJSON(a.out, report.CodeUsage, v.flag+" has no JSON output; "+v.hint, 2)
		}
	}
	a.workRepo()                        // look the repository up while listing
	return a.list(a.out, a.termWidth()) // one write: the envelope
}

// fileJSON is -A --json: one file's facts and outline.
func (a *app) fileJSON() int {
	o := &a.o
	usage := func(format string, args ...any) int {
		return a.failJSON(a.out, report.CodeUsage, fmt.Sprintf(format, args...), 2)
	}
	if o.since != "" {
		if _, err := parseAge(o.since); err != nil {
			return usage("%v", err)
		}
	}
	if o.budget < 1 {
		return usage("--budget must be at least 1 (got %d)", o.budget)
	}
	if o.treemap || o.du {
		return usage("-A --json has no disk usage output; for directory sizes, use glint --json --du")
	}
	paths := a.flags.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	if len(paths) > 1 {
		return usage("-A --json takes one file, got %d paths", len(paths))
	}
	path := paths[0]
	here := a.workRepo() // started before the file is read, since it runs git
	st, err := os.Stat(path)
	if err != nil {
		return a.failJSON(a.out, report.CodeFailed, fmt.Sprintf("cannot access %s: %s", path, render.ErrMessage(err)), 2)
	}
	if st.IsDir() {
		return usage("-A --json takes a file and prints its outline; for a directory, use glint --json %s (its listing)", path)
	}
	file, truncated := agent.File(path, st, agent.Options{NoGit: o.noGit, Heat: o.heat}, here, report.MaxItems)
	env := a.envelope()
	env.Result, env.Truncated = report.Glint{File: &file}, truncated
	return a.writeJSON(a.out, env)
}

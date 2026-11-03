// Package cli wires flags, loading and rendering together.
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/muesli/termenv"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/Vaspyyy/glint/internal/agent"
	"github.com/Vaspyyy/glint/internal/config"
	"github.com/Vaspyyy/glint/internal/dash"
	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/glint/internal/img"
	"github.com/Vaspyyy/glint/internal/peek"
	"github.com/Vaspyyy/glint/internal/project"
	"github.com/Vaspyyy/glint/internal/render"
	"github.com/Vaspyyy/glint/internal/theme"
	"github.com/Vaspyyy/glint/internal/tui"
)

const version = "0.5.1"

type options struct {
	all                         int
	long, oneline, tree         bool
	level                       int
	sortBy                      string
	sortTime, sortSize, sortExt bool
	reverse                     bool
	groupDirs                   string
	onlyDirs, onlyFiles         bool
	listDirs                    bool
	ignore                      []string
	classify, bytes, du, bars   bool
	git, noGit, gitignore       bool
	commits, heat, changed      bool
	since                       string
	peek, card, treemap         bool
	thumbs                      string
	interactive, watch          bool
	agentMode, outline          bool
	projects                    bool
	budget                      int
	icons, color, theme         string
	timeStyle                   string
	octal, inode, links         bool
	noOwner, group              bool
	hyperlink                   bool
	noHeader, noSummary         bool
	json                        bool
	width                       int
	initShell                   string
	autoLs                      bool
	configPath                  string
	noConfig                    bool
	help, version               bool
}

func newFlagSet(o *options) *pflag.FlagSet {
	f := pflag.NewFlagSet("glint", pflag.ContinueOnError)
	f.SortFlags = false
	f.SetOutput(io.Discard)

	f.BoolVarP(&o.long, "long", "l", false, "detailed table view")
	f.BoolVarP(&o.oneline, "oneline", "1", false, "one entry per line")
	f.BoolVarP(&o.tree, "tree", "T", false, "recurse into directories as a tree")
	f.IntVarP(&o.level, "level", "L", 0, "limit tree depth (0 = unlimited)")
	f.BoolVarP(&o.interactive, "interactive", "i", false, "browse interactively with previews")
	f.BoolVarP(&o.projects, "projects", "P", false, "dashboard of a folder of projects: stack, git state, activity")
	f.BoolVarP(&o.treemap, "map", "M", false, "disk usage treemap")
	f.StringVar(&o.thumbs, "thumbs", "", "image thumbnail grid: auto, kitty, blocks")
	f.Lookup("thumbs").NoOptDefVal = "auto"
	f.BoolVarP(&o.card, "card", "C", false, "project card: stack, git, languages, TODOs")
	f.BoolVarP(&o.watch, "watch", "W", false, "keep the listing live as files change")
	f.BoolVarP(&o.agentMode, "agent", "A", false, "compact, token-efficient output for AI agents")
	f.IntVar(&o.budget, "budget", 200, "with --agent: approximate line budget for the tree")
	f.BoolVar(&o.outline, "outline", false, "with --agent: list symbols (funcs, types) with line numbers")
	f.BoolVar(&o.json, "json", false, "machine-readable JSON output")
	f.IntVarP(&o.width, "width", "w", 0, "override terminal width")

	f.CountVarP(&o.all, "all", "a", "show hidden files (-aa also shows . and ..)")
	f.BoolVarP(&o.onlyDirs, "only-dirs", "D", false, "list only directories")
	f.BoolVarP(&o.onlyFiles, "only-files", "f", false, "list only files")
	f.BoolVarP(&o.listDirs, "directory", "d", false, "list directories themselves, not their contents")
	f.StringSliceVarP(&o.ignore, "ignore", "I", nil, "hide entries matching a glob (repeatable)")
	f.BoolVar(&o.gitignore, "gitignore", false, "hide files ignored by git")
	f.BoolVar(&o.changed, "changed", false, "only files with uncommitted git changes")
	f.StringVar(&o.since, "since", "", "only entries modified within e.g. 30m, 2h, 3d, 1w, 6mo")

	f.StringVarP(&o.sortBy, "sort", "s", "name", "sort by: "+strings.Join(fsx.SortKeys, ", "))
	f.BoolVarP(&o.sortTime, "sort-time", "t", false, "sort by modification time, newest first")
	f.BoolVarP(&o.sortSize, "sort-size", "S", false, "sort by size, largest first")
	f.BoolVarP(&o.sortExt, "sort-ext", "X", false, "sort by extension")
	f.BoolVarP(&o.reverse, "reverse", "r", false, "reverse the sort order")
	f.StringVar(&o.groupDirs, "group-dirs", "first", "directories: first, last, none")

	f.BoolVarP(&o.peek, "peek", "p", false, "describe contents: project stack, lines, dimensions, duration…")
	f.BoolVarP(&o.git, "git", "g", false, "show git status (auto in long view)")
	f.BoolVarP(&o.commits, "commits", "c", false, "last commit touching each entry")
	f.BoolVarP(&o.heat, "heat", "H", false, "git churn: how often each entry changes")
	f.BoolVar(&o.noGit, "no-git", false, "never run git")
	f.BoolVar(&o.du, "du", false, "show total size of directories (with size bars in -l)")
	f.BoolVar(&o.bars, "bars", false, "size bars in the long view")
	f.BoolVarP(&o.bytes, "bytes", "B", false, "exact byte sizes")
	f.StringVar(&o.timeStyle, "time-style", "relative", "relative, iso, long-iso, full, ls")
	f.BoolVar(&o.octal, "octal", false, "permissions as octal (0755)")
	f.BoolVar(&o.inode, "inode", false, "show inode numbers")
	f.BoolVar(&o.links, "links", false, "show hard link counts")
	f.BoolVar(&o.group, "group", false, "show group column")
	f.BoolVar(&o.noOwner, "no-owner", false, "hide user column")
	f.BoolVar(&o.noHeader, "no-header", false, "hide title and column headers")
	f.BoolVar(&o.noSummary, "no-summary", false, "hide the summary footer")

	f.StringVar(&o.icons, "icons", "auto", "Nerd Font icons: auto, always, never")
	f.StringVar(&o.color, "color", "auto", "colors: auto, always, never")
	f.StringVar(&o.theme, "theme", "catppuccin", "palette: "+strings.Join(theme.Names(), ", ")+" or custom")
	f.BoolVarP(&o.classify, "classify", "F", false, "append indicator (/ @ * | =)")
	f.BoolVar(&o.hyperlink, "hyperlink", false, "make names clickable (OSC 8)")

	f.StringVar(&o.initShell, "init", "", "print shell integration (fish) or write a sample config (config)")
	f.BoolVar(&o.autoLs, "auto-ls", false, "with --init fish: list the directory after every cd")
	f.StringVar(&o.configPath, "config", config.Path(), "config file")
	f.BoolVar(&o.noConfig, "no-config", false, "ignore the config file")
	f.BoolVarP(&o.help, "help", "h", false, "show this help")
	f.BoolVarP(&o.version, "version", "v", false, "show version")
	return f
}

func oneOf(name, val string, allowed ...string) error {
	if slices.Contains(allowed, val) {
		return nil
	}
	return fmt.Errorf("invalid --%s %q (want %s)", name, val, strings.Join(allowed, ", "))
}

// parseAge understands 45s, 30m, 2h, 3d, 1w, 6mo, 1y.
func parseAge(s string) (time.Duration, error) {
	units := []struct {
		suffix string
		d      time.Duration
	}{{"mo", 30 * 24 * time.Hour}, {"s", time.Second}, {"m", time.Minute}, {"h", time.Hour},
		{"d", 24 * time.Hour}, {"w", 7 * 24 * time.Hour}, {"y", 365 * 24 * time.Hour}}
	for _, u := range units {
		if num, ok := strings.CutSuffix(s, u.suffix); ok {
			n, err := strconv.ParseFloat(num, 64)
			if err != nil || n < 0 {
				break
			}
			return time.Duration(n * float64(u.d)), nil
		}
	}
	return 0, fmt.Errorf("invalid --since %q (try 30m, 2h, 3d, 1w, 6mo)", s)
}

type app struct {
	o       options
	flags   *pflag.FlagSet
	painter *theme.Painter
	isTTY   bool
	since   time.Duration
	kitty   bool
	errOut  io.Writer
}

func Run(args []string) int {
	if env := os.Getenv("GLINT_OPTS"); env != "" {
		args = append(strings.Fields(env), args...)
	}
	a := &app{errOut: os.Stderr}
	a.flags = newFlagSet(&a.o)
	die := func(err error) int {
		fmt.Fprintf(a.errOut, "glint: %v\nTry 'glint --help'.\n", err)
		return 2
	}

	// the config file supplies defaults, so it must be applied before parsing
	cfgPath, useCfg := config.Path(), true
	for i, arg := range args {
		switch {
		case arg == "--no-config":
			useCfg = false
		case arg == "--config" && i+1 < len(args):
			cfgPath = args[i+1]
		case strings.HasPrefix(arg, "--config="):
			cfgPath = strings.TrimPrefix(arg, "--config=")
		}
	}
	if useCfg {
		if err := config.Load(cfgPath, a.flags); err != nil {
			return die(err)
		}
	}
	if err := a.flags.Parse(args); err != nil {
		return die(err)
	}
	o := &a.o
	if o.agentMode {
		return a.agent()
	}
	// commit/heat columns only exist in the table, so asking for them implies it
	if (o.commits || o.heat) && !o.json && !o.interactive {
		o.long = true
	}

	out := os.Stdout
	a.isTTY = term.IsTerminal(int(out.Fd()))
	for _, c := range [][]string{
		{"color", o.color, "auto", "always", "never"},
		{"icons", o.icons, "auto", "always", "never"},
		{"group-dirs", o.groupDirs, "first", "last", "none"},
		{"time-style", o.timeStyle, "relative", "iso", "long-iso", "full", "ls"},
		append([]string{"sort", o.sortBy}, fsx.SortKeys...),
	} {
		if err := oneOf(c[0], c[1], c[2:]...); err != nil {
			return die(err)
		}
	}
	if o.thumbs != "" {
		if err := oneOf("thumbs", o.thumbs, "auto", "kitty", "blocks"); err != nil {
			return die(err)
		}
		a.kitty = o.thumbs == "kitty" || (o.thumbs == "auto" && img.KittySupported())
	}
	if o.since != "" {
		d, err := parseAge(o.since)
		if err != nil {
			return die(err)
		}
		a.since = d
	}
	pal, err := theme.Lookup(o.theme)
	if err != nil {
		return die(err)
	}

	var profile termenv.Profile
	switch o.color {
	case "always":
		profile = termenv.ANSI256
		if ct := os.Getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
			profile = termenv.TrueColor
		}
	case "never":
		profile = termenv.Ascii
	default:
		profile = termenv.Ascii
		if a.isTTY || o.interactive {
			profile = termenv.NewOutput(os.Stderr).EnvColorProfile()
			if a.isTTY {
				profile = termenv.NewOutput(out).EnvColorProfile()
			}
		}
	}
	a.painter = theme.NewPainter(profile, pal)

	switch {
	case o.help:
		printHelp(out, a.flags, a.painter)
		return 0
	case o.version:
		fmt.Fprintln(out, "glint", version)
		return 0
	case o.initShell != "":
		return a.init(out)
	case o.interactive:
		return a.interactive()
	case o.watch:
		return a.watchLoop()
	}
	w := bufio.NewWriterSize(out, 64<<10)
	defer w.Flush()
	if o.projects {
		return a.dashboard(w, a.termWidth())
	}
	return a.list(w, a.termWidth())
}

func (a *app) dashboard(w io.Writer, width int) int {
	o := &a.o
	key := "time"
	switch {
	case o.sortSize:
		key = "size"
	case o.sortBy == "name" && !o.sortTime:
		if a.flags.Changed("sort") {
			key = "name"
		}
	}
	r := render.New(a.renderConfig(width))
	paths := a.flags.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	status := 0
	for i, p := range paths {
		if i > 0 {
			fmt.Fprintln(w)
		}
		e, err := fsx.Stat(p, fsx.Options{})
		if err != nil || !e.IsDir {
			fmt.Fprintf(a.errOut, "glint: %s is not a directory\n", p)
			status = 2
			continue
		}
		rows, err := dash.Scan(p, o.all > 0)
		if err != nil {
			fmt.Fprintf(a.errOut, "glint: %s: %s\n", p, render.ErrMessage(err))
			status = 2
			continue
		}
		dash.Sort(rows, key, o.reverse)
		r.Dashboard(w, e, rows)
	}
	return status
}

func (a *app) agent() int {
	o := &a.o
	var since time.Duration
	if o.since != "" {
		d, err := parseAge(o.since)
		if err != nil {
			fmt.Fprintf(a.errOut, "glint: %v\n", err)
			return 2
		}
		since = d
	}
	if o.budget < 1 {
		fmt.Fprintf(a.errOut, "glint: --budget must be at least 1 (got %d)\n", o.budget)
		return 2
	}
	paths := a.flags.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	opt := agent.Options{
		Budget: o.budget, MaxDepth: o.level, All: o.all > 0, Since: since, SinceRaw: o.since,
		Changed: o.changed, Commits: o.commits, Outline: o.outline, Ignore: o.ignore, NoGit: o.noGit, Long: o.long,
	}
	w := bufio.NewWriterSize(os.Stdout, 64<<10)
	defer w.Flush()
	if o.treemap || o.du {
		return agent.DiskUsage(w, paths, opt)
	}
	return agent.Run(w, paths, opt)
}

func (a *app) termWidth() int {
	if a.o.width > 0 {
		return a.o.width
	}
	if a.isTTY {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			return w
		}
	}
	if c, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && c > 0 {
		return c
	}
	return 80
}

func (a *app) termHeight() int {
	if _, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && h > 0 {
		return h
	}
	return 30
}

func (a *app) renderConfig(width int) render.Config {
	o := &a.o
	home, _ := os.UserHomeDir()
	return render.Config{
		Painter:   a.painter,
		Icons:     o.icons == "always" || (o.icons == "auto" && (a.isTTY || o.interactive)),
		Classify:  o.classify,
		Bytes:     o.bytes,
		TimeStyle: o.timeStyle,
		Octal:     o.octal,
		Inode:     o.inode,
		Links:     o.links,
		Owner:     o.long && !o.noOwner,
		Group:     o.long && o.group,
		Header:    !o.noHeader,
		Hyperlink: o.hyperlink,
		Peek:      o.peek,
		Commits:   o.commits && o.long,
		Heat:      o.heat && o.long,
		Bars:      o.long && (o.bars || o.du),
		Width:     width,
		Height:    a.termHeight(),
		Now:       time.Now(),
		Home:      home,
	}
}

func (a *app) interactive() int {
	o := &a.o
	start := "."
	if args := a.flags.Args(); len(args) > 0 {
		start = args[0]
	}
	if st, err := os.Stat(start); err != nil || !st.IsDir() {
		fmt.Fprintf(a.errOut, "glint: %s is not a directory\n", start)
		return 2
	}
	sortKey := o.sortBy
	switch {
	case o.sortTime:
		sortKey = "time"
	case o.sortSize:
		sortKey = "size"
	}
	dir, err := tui.Run(tui.Options{
		Painter: a.painter, Render: a.renderConfig(100), Start: start,
		Hidden: o.all > 0, SortKey: sortKey, Map: o.treemap,
	})
	if err != nil {
		fmt.Fprintln(a.errOut, "glint:", err)
		return 1
	}
	// when captured by the `gi` shell function, hand the final directory back
	if dir != "" && !a.isTTY {
		fmt.Println(dir)
	}
	return 0
}

// list renders one full listing into w and returns the exit status.
func (a *app) list(w io.Writer, width int) int {
	o := &a.o
	r := render.New(a.renderConfig(width))
	errOut := a.errOut
	painter := a.painter

	sortKey := o.sortBy
	switch {
	case o.sortTime:
		sortKey = "time"
	case o.sortSize:
		sortKey = "size"
	case o.sortExt:
		sortKey = "ext"
	}
	if o.treemap {
		sortKey = "size"
	}

	showOwner := o.long && !o.noOwner
	showGroup := o.long && o.group
	lopts := fsx.Options{
		Hidden:    o.all,
		Owners:    showOwner || showGroup || o.json,
		OnlyDirs:  o.onlyDirs,
		OnlyFiles: o.onlyFiles,
		Ignore:    o.ignore,
	}
	history := (o.commits || o.heat) && (o.long || o.json)
	wantGit := !o.noGit && (o.git || o.gitignore || o.long || o.json || o.changed || history || o.card)
	showGit := !o.noGit && (o.git || o.long)
	thumbs := o.thumbs != "" && a.isTTY
	// peek text needs a line per entry, so it switches the grid to a list
	grid := !o.long && !o.oneline && !o.tree && !o.peek && a.isTTY
	summary := !o.noSummary && a.isTTY && (o.long || o.tree)
	du := o.du
	cutoff := time.Now().Add(-a.since)

	status := 0
	fail := func(path string, err error) {
		if bw, ok := w.(*bufio.Writer); ok {
			bw.Flush()
		}
		red := painter.Fg(theme.Red)
		fmt.Fprintf(errOut, "%s cannot access %s: %s\n", red.Bold().Render("glint:"), path, red.Render(render.ErrMessage(err)))
		status = 2
	}

	openRepo := func(dir string) *git.Repo {
		if !wantGit {
			return nil
		}
		repo, err := git.Open(dir)
		if err != nil {
			return nil
		}
		return repo
	}
	keepRecent := func(e *fsx.Entry) bool { return a.since == 0 || e.ModTime.After(cutoff) }
	keepChanged := func(e *fsx.Entry) bool { return !o.changed || (!e.Git.Clean() && !e.Git.Ignored()) }

	prepare := func(repo *git.Repo) func([]*fsx.Entry) []*fsx.Entry {
		return func(es []*fsx.Entry) []*fsx.Entry {
			kept := es[:0]
			for _, e := range es {
				if repo != nil {
					e.Git = repo.Status(e.Abs)
					if o.gitignore && e.Git.Ignored() {
						continue
					}
				}
				// in tree mode directories survive here and get pruned once their subtree is known
				if !(o.tree && e.IsDir) && (!keepRecent(e) || !keepChanged(e)) {
					continue
				}
				kept = append(kept, e)
			}
			es = kept
			if du {
				fsx.ComputeDirSizes(es)
			}
			if o.peek || thumbs {
				peek.Compute(es)
			}
			if history && repo != nil && len(es) > 0 {
				dir := filepath.Dir(es[0].Path)
				names := make([]string, 0, len(es))
				for _, e := range es {
					if filepath.Dir(e.Path) == dir && !e.Git.Ignored() && !e.Git.Untracked() {
						names = append(names, e.Name)
					}
				}
				commits := repo.History(dir, names, o.heat)
				for _, e := range es {
					e.Commit = commits[e.Name]
				}
			}
			fsx.Sort(es, sortKey, o.reverse, o.groupDirs)
			return es
		}
	}

	paths := a.flags.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	var files, dirs []*fsx.Entry
	for _, p := range paths {
		e, err := fsx.Stat(p, lopts)
		if err != nil {
			fail(p, err)
			continue
		}
		if e.IsDir && !o.listDirs {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}

	if o.json {
		var all []*fsx.Entry
		if len(files) > 0 {
			all = prepare(openRepo("."))(files)
		}
		for _, d := range dirs {
			prep := prepare(openRepo(d.Path))
			if o.tree {
				fsx.BuildTree(d, lopts, o.level, prep)
				d.Children = fsx.Prune(d.Children, func(e *fsx.Entry) bool { return keepRecent(e) && keepChanged(e) })
			} else if kids, err := fsx.ReadDir(d.Path, lopts); err != nil {
				d.ChildErr = err
			} else {
				d.Children = prep(kids)
			}
			all = append(all, d)
		}
		if len(files) == 0 && len(dirs) == 1 && dirs[0].ChildErr == nil {
			all = dirs[0].Children
			if all == nil {
				all = []*fsx.Entry{}
			}
		}
		if err := render.JSON(w, all); err != nil {
			fail("stdout", err)
		}
		return status
	}

	list := func(es []*fsx.Entry) {
		switch {
		case o.long:
			if len(es) == 0 {
				fmt.Fprintln(w, r.Empty())
				return
			}
			r.Long(w, render.Rows(es))
		case thumbs:
			r.Thumbs(w, es, a.kitty)
		case grid:
			r.Grid(w, es)
		default:
			r.List(w, render.Rows(es))
		}
		if summary && len(es) > 0 {
			fmt.Fprintln(w, "\n"+r.Summary(es))
		}
	}

	if len(files) > 0 {
		repo := openRepo(".")
		r.SetGit(showGit && repo != nil)
		list(prepare(repo)(files))
	}

	multi := len(files)+len(dirs) > 1
	for i, d := range dirs {
		if i > 0 || len(files) > 0 {
			fmt.Fprintln(w)
		}
		repo := openRepo(d.Path)
		r.SetGit(showGit && repo != nil)
		prep := prepare(repo)

		header := func() {
			if o.card {
				name := filepath.Base(d.Abs)
				fmt.Fprintln(w, r.Card(project.Collect(d.Path, repo), name))
			} else {
				fmt.Fprintln(w, r.Title(d, repo))
			}
		}

		if o.tree {
			fsx.BuildTree(d, lopts, o.level, prep)
			if a.since > 0 || o.changed {
				d.Children = fsx.Prune(d.Children, func(e *fsx.Entry) bool { return !e.IsDir && keepRecent(e) && keepChanged(e) })
			}
			header()
			rows := render.Flatten(d)
			switch {
			case len(rows) == 0:
				fmt.Fprintln(w, r.Empty())
			case o.long:
				r.Long(w, rows)
			default:
				r.List(w, rows)
			}
			if summary && len(d.Children) > 0 {
				fmt.Fprintln(w, "\n"+r.Summary(d.Children))
			}
			continue
		}

		es, err := fsx.ReadDir(d.Path, lopts)
		if err != nil {
			fail(d.Path, err)
			continue
		}
		es = prep(es)

		if o.treemap {
			regens := fsx.DiskUsage(es)
			var total int64
			for _, e := range es {
				total += e.DiskSize
			}
			fmt.Fprintln(w, r.Title(d, repo)+painter.Fg(theme.Faint).Render(" · ")+
				painter.Fg(theme.Text).Bold().Render(render.HumanSize(total))+painter.Fg(theme.Faint).Render(" on disk"))
			// fill the window: title above; the reclaim callout and a prompt below
			callout := 0
			for _, g := range regens {
				if callout == 6 || g.Size < 16<<20 { // same cut-off as render.Reclaim
					break
				}
				callout++
			}
			below := 2
			if callout > 0 {
				below += 2 + callout
			}
			height := max(8, min(a.termHeight()-1-below, 60))
			if !a.isTTY {
				height = 20
			}
			r.Treemap(w, es, regens, width, height)
			continue
		}

		if o.card || (o.long && !o.noHeader) || multi {
			header()
		}
		list(es)
	}
	return status
}

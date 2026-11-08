package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/pflag"

	"github.com/Vaspyyy/glint/internal/config"
	"github.com/Vaspyyy/glint/internal/textw"
	"github.com/Vaspyyy/glint/internal/theme"
)

var helpGroups = []struct {
	title string
	flags []string
}{
	{"Views", []string{"long", "oneline", "tree", "level", "interactive", "projects", "map", "thumbs", "card", "watch", "json", "width"}},
	{"For agents", []string{"agent", "budget", "outline", "no-context"}},
	{"Filtering", []string{"all", "only-dirs", "only-files", "directory", "ignore", "gitignore", "changed", "since"}},
	{"Sorting", []string{"sort", "sort-time", "sort-size", "sort-ext", "reverse", "group-dirs"}},
	{"Details", []string{"peek", "git", "commits", "heat", "no-git", "du", "bars", "bytes", "time-style", "octal", "inode", "links", "group", "no-owner", "no-header", "no-summary"}},
	{"Appearance", []string{"icons", "color", "theme", "classify", "hyperlink"}},
	{"Setup", []string{"init", "auto-ls", "config", "no-config", "help", "version"}},
}

func printHelp(w io.Writer, flags *pflag.FlagSet, p *theme.Painter) {
	title := p.Fg(theme.Purple).Bold()
	head := p.Fg(theme.Blue).Bold()
	short := p.Fg(theme.Green)
	long := p.Fg(theme.Cyan)
	faint := p.Fg(theme.Faint)
	text := p.Fg(theme.Text)

	fmt.Fprintf(w, "\n  %s %s  %s\n\n", title.Render("✦ glint"), faint.Render("v"+version), p.Fg(theme.Subtle).Italic().Render("ls, but make it pretty"))
	fmt.Fprintf(w, "  %s\n    glint %s %s\n", head.Render("USAGE"), faint.Render("[flags]"), faint.Render("[path...]"))

	const col = 32
	for _, g := range helpGroups {
		fmt.Fprintf(w, "\n  %s\n", head.Render(strings.ToUpper(g.title)))
		for _, name := range g.flags {
			f := flags.Lookup(name)
			if f == nil {
				continue
			}
			left := "    "
			if f.Shorthand != "" {
				left += short.Render("-"+f.Shorthand) + faint.Render(", ")
			} else {
				left += "    "
			}
			left += long.Render("--" + f.Name)
			if t := f.Value.Type(); t != "bool" && t != "count" {
				left += " " + faint.Render(strings.TrimSuffix(t, "Slice"))
			}
			usage := text.Render(f.Usage)
			if d := f.DefValue; d != "" && d != "false" && d != "0" && d != "[]" && !strings.Contains(f.Usage, d+",") {
				usage += faint.Render(" (default " + d + ")")
			}
			fmt.Fprintf(w, "%s%s%s\n", left, strings.Repeat(" ", max(1, col-textw.Width(left))), usage)
		}
	}

	fmt.Fprintf(w, "\n  %s\n", head.Render("EXAMPLES"))
	for _, ex := range [][2]string{
		{"glint -la", "everything, in detail, with git status"},
		{"glint -P ~/Projects", "dashboard: every project's git state"},
		{"glint -p ~/Projects", "what is each of these projects?"},
		{"glint -i", "browse: f search · P projects · M disk map"},
		{"glint -M ~/Downloads", "what's eating my disk?"},
		{"glint -M -i ~", "explore disk usage interactively"},
		{"glint -lcH", "last commits + git churn per file"},
		{"glint -C", "project card for this repo"},
		{"glint -T --changed", "tree of uncommitted changes"},
		{"glint --since 2h -W", "live view of recent files"},
		{"glint --thumbs ~/Pictures", "image thumbnails"},
		{"glint -A", "repo map for AI agents (-A file.go: outline)"},
	} {
		fmt.Fprintf(w, "    %s%s%s\n", long.Render(ex[0]), strings.Repeat(" ", max(1, col-4-len(ex[0]))), faint.Render(ex[1]))
	}
	fmt.Fprintf(w, "\n  %s\n    %s  %s\n    %s      %s\n\n", head.Render("ENVIRONMENT"),
		long.Render("GLINT_OPTS"), faint.Render("extra default flags, e.g. GLINT_OPTS=\"--theme nord -F\""),
		long.Render("config"), faint.Render(config.Path()+"  (glint --init config)"))
}

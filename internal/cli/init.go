package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"

	"github.com/Vaspyyy/glint/internal/config"
	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/theme"
)

func (a *app) init(w io.Writer) int {
	switch a.o.initShell {
	case "fish":
		fishInit(w, a.flags, a.o.autoLs)
		return 0
	case "config":
		path := a.o.configPath
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(a.errOut, "glint: %s already exists\n", path)
			return 1
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintln(a.errOut, "glint:", err)
			return 1
		}
		if err := os.WriteFile(path, []byte(config.Sample), 0o644); err != nil {
			fmt.Fprintln(a.errOut, "glint:", err)
			return 1
		}
		fmt.Fprintln(w, "wrote", path)
		return 0
	case "copy":
		return a.installCopyHandler(w)
	}
	fmt.Fprintf(a.errOut, "glint: --init wants fish, config or copy, got %q\n", a.o.initShell)
	return 2
}

var enumValues = map[string][]string{
	"color":      {"auto", "always", "never"},
	"icons":      {"auto", "always", "never"},
	"thumbs":     {"auto", "kitty", "blocks"},
	"group-dirs": {"first", "last", "none"},
	"time-style": {"relative", "iso", "long-iso", "full", "ls"},
	"init":       {"fish", "config", "copy"},
	"hyperlink":  hyperlinkModes,
}

func fishQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `\'`) + "'" }

func fishInit(w io.Writer, flags *pflag.FlagSet, autoLs bool) {
	fmt.Fprintln(w, "# glint shell integration: add `glint --init fish | source` to config.fish")
	fmt.Fprintln(w, "complete -c glint -e")
	enumValues["sort"] = fsx.SortKeys
	enumValues["theme"] = theme.Names()
	flags.VisitAll(func(f *pflag.Flag) {
		line := "complete -c glint"
		if f.Shorthand != "" {
			line += " -s " + f.Shorthand
		}
		line += " -l " + f.Name
		if vals, ok := enumValues[f.Name]; ok {
			line += " -x -a " + fishQuote(strings.Join(vals, " "))
		} else if t := f.Value.Type(); t != "bool" && t != "count" {
			line += " -r"
		}
		line += " -d " + fishQuote(f.Usage)
		fmt.Fprintln(w, line)
	})
	fmt.Fprint(w, `
function gi --description 'browse with glint, cd into the folder you quit in'
    set -l dir (command glint -i $argv)
    and test -n "$dir"; and test -d "$dir"; and cd $dir
end
`)
	if autoLs {
		fmt.Fprint(w, `
function __glint_autols --on-variable PWD --description 'glint after every cd'
    status --is-command-substitution; and return
    status is-interactive; or return
    command glint
end
`)
	}
}

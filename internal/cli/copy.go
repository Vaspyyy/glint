package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Vaspyyy/glint/internal/clip"
	"github.com/Vaspyyy/glint/internal/config"
)

// copyFiles is `glint --copy`: it puts the named files on the clipboard.
// An argument can be a glint-copy: link, which is what a click on a name
// passes (see installCopyHandler). A click has no terminal, so its result
// is also shown as a notification.
func (a *app) copyFiles() int {
	args := a.flags.Args()
	if len(args) == 0 {
		fmt.Fprintln(a.errOut, "glint: --copy wants a file\nTry 'glint --help'.")
		return 2
	}
	clicked := false
	fail := func(err error) int {
		fmt.Fprintf(a.errOut, "glint: %v\n", err)
		if clicked {
			clip.Notify("Couldn't copy", err.Error())
		}
		return 1
	}
	paths := make([]string, 0, len(args))
	for _, arg := range args {
		p, link, err := clip.Resolve(arg)
		clicked = clicked || link
		if err != nil {
			return fail(err)
		}
		paths = append(paths, p)
	}
	if err := clip.Copy(paths); err != nil {
		return fail(err)
	}
	what := clip.Describe(paths)
	fmt.Fprintf(a.out, "copied %s to the clipboard\n", what)
	if clicked {
		body := filepath.Dir(paths[0])
		if home, err := os.UserHomeDir(); err == nil && home != "/" {
			if rest, ok := strings.CutPrefix(body, home); ok && (rest == "" || rest[0] == '/') {
				body = "~" + rest
			}
		}
		if len(paths) > 1 {
			names := make([]string, len(paths))
			for i, p := range paths {
				names[i] = filepath.Base(p)
			}
			body = strings.Join(names, ", ")
		}
		clip.Notify("Copied "+what, body)
	}
	return 0
}

const copyHandler = "glint-copy.desktop"

// desktopArg quotes an argument for a desktop entry's Exec line.
func desktopArg(s string) string {
	plain := s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+@:,", r))
	})
	if plain {
		return s
	}
	// quoting rules of the Exec key, on top of a string value's own escaping
	// of the backslash
	return `"` + strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", `$`, `\\$`, `%`, `%%`).Replace(s) + `"`
}

// installCopyHandler is `glint --init copy`: it registers this glint as
// what opens glint-copy: links, so that a terminal's click on a name
// printed with --hyperlink=copy runs `glint --copy <link>`. The entry
// passes --no-config: a click copies, whatever the config file sets.
func (a *app) installCopyHandler(w io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(a.errOut, "glint:", err)
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fail(err)
		}
		data = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(data, "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(err)
	}
	entry := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=glint: copy file\n" +
		"Comment=Copies the file a glint-copy: link names to the clipboard\n" +
		"Exec=" + desktopArg(exe) + " --no-config --copy %u\n" +
		"Icon=edit-copy\n" +
		"NoDisplay=true\n" +
		"Terminal=false\n" +
		"MimeType=x-scheme-handler/" + clip.Scheme + ";\n"
	path := filepath.Join(dir, copyHandler)
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return fail(err)
	}
	fmt.Fprintln(w, "wrote", path)

	mime := "x-scheme-handler/" + clip.Scheme
	if tool, err := exec.LookPath("xdg-mime"); err != nil {
		fmt.Fprintf(w, "xdg-mime is not installed: make %s the default for %s yourself\n", copyHandler, mime)
	} else if out, err := exec.Command(tool, "default", copyHandler, mime).CombinedOutput(); err != nil {
		return fail(fmt.Errorf("xdg-mime default %s %s: %v\n%s", copyHandler, mime, err, strings.TrimSpace(string(out))))
	}
	if tool, err := exec.LookPath("update-desktop-database"); err == nil {
		exec.Command(tool, dir).Run() // a cache some desktops read; fine without
	}
	fmt.Fprintf(w, "%s links now run: %s --copy <link>\n\n", clip.Scheme, exe)
	fmt.Fprintf(w, "Turn click-to-copy on with\n    hyperlink = \"copy\"\nin %s, or for one listing with glint --hyperlink=copy.\n", config.Path())
	return 0
}

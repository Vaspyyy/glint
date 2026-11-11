//go:build unix

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Vaspyyy/glint/internal/clip"
)

// desktop gives a test a Wayland session whose tools are stand-ins that
// write down how they were called, and returns the folder they are in.
func desktop(t *testing.T, tools ...string) string {
	t.Helper()
	isolate(t)
	bin := t.TempDir()
	for _, tool := range tools {
		script := "#!/bin/sh\nfor a; do echo \"$a\"; done > \"$0.args\"\n"
		if tool == "wl-copy" {
			script += "/bin/cat > \"$0.in\"\n"
		}
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("WAYLAND_DISPLAY", "test")
	t.Setenv("DISPLAY", "")
	return bin
}

// realDir is a new temp folder by the path the working directory reports
// once the test is in it: on macOS the temp folder is behind a symlink.
func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func glint(args ...string) (stdout, stderr string, status int) {
	var out, errOut bytes.Buffer
	status = run(args, &out, &errOut)
	return out.String(), errOut.String(), status
}

func slurp(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func TestCopyFromTheCommandLine(t *testing.T) {
	bin := desktop(t, "wl-copy", "notify-send")
	dir := realDir(t)
	write(t, filepath.Join(dir, "README.md"), "hi\n")
	write(t, filepath.Join(dir, "main.go"), "package main\n")
	t.Chdir(dir)

	out, errOut, status := glint("--copy", "README.md")
	if status != 0 || errOut != "" || out != "copied README.md to the clipboard\n" {
		t.Fatalf("status %d, stdout %q, stderr %q", status, out, errOut)
	}
	if got, want := slurp(filepath.Join(bin, "wl-copy.in")), "file://"+dir+"/README.md\r\n"; got != want {
		t.Errorf("clipboard got %q, want %q", got, want)
	}
	if got := slurp(filepath.Join(bin, "notify-send.args")); got != "" {
		t.Errorf("a copy typed in a terminal sent a notification: %q", got)
	}

	out, _, status = glint("--copy", "README.md", "main.go")
	if status != 0 || out != "copied 2 files to the clipboard\n" {
		t.Errorf("two files: status %d, stdout %q", status, out)
	}
	if got, want := slurp(filepath.Join(bin, "wl-copy.in")), "file://"+dir+"/README.md\r\nfile://"+dir+"/main.go\r\n"; got != want {
		t.Errorf("clipboard got %q, want %q", got, want)
	}
}

func TestCopyFailures(t *testing.T) {
	bin := desktop(t, "wl-copy", "notify-send")
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), "")
	t.Chdir(dir)
	for _, c := range []struct {
		args   []string
		status int
		errHas string
	}{
		{[]string{"--copy"}, 2, "--copy wants a file"},
		{[]string{"--copy", "gone.txt"}, 1, "gone.txt: no such file"},
		{[]string{"--copy", "a.txt", "gone.txt"}, 1, "gone.txt: no such file"},
		{[]string{"--copy", "--json", "a.txt"}, 2, "--copy has no JSON output"},
	} {
		out, errOut, status := glint(c.args...)
		if status != c.status || !strings.Contains(out+errOut, c.errHas) {
			t.Errorf("glint %v: status %d, stdout %q, stderr %q; want status %d and %q", c.args, status, out, errOut, c.status, c.errHas)
		}
		if got := slurp(filepath.Join(bin, "wl-copy.in")); got != "" {
			t.Errorf("glint %v put %q on the clipboard", c.args, got)
		}
	}
}

// A click on a name runs `glint --copy <link>` with no terminal, so the
// answer is a notification, also when the copy didn't work.
func TestCopyFromAClickedLink(t *testing.T) {
	bin := desktop(t, "wl-copy", "notify-send")
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "proj")
	file := filepath.Join(dir, "two words.md")
	write(t, file, "hi\n")

	if _, errOut, status := glint("--copy", clip.Link(file)); status != 0 {
		t.Fatalf("status %d, stderr %q", status, errOut)
	}
	if got, want := slurp(filepath.Join(bin, "wl-copy.in")), "file://"+dir+"/two%20words.md\r\n"; got != want {
		t.Errorf("clipboard got %q, want %q", got, want)
	}
	note := slurp(filepath.Join(bin, "notify-send.args"))
	if !strings.HasSuffix(note, "--\nCopied two words.md\n~/proj\n") {
		t.Errorf("notification: %q", note)
	}

	os.Remove(filepath.Join(bin, "wl-copy.in"))
	_, errOut, status := glint("--copy", clip.Scheme+"://elsewhere"+file)
	if status != 1 || !strings.Contains(errOut, "another machine") {
		t.Errorf("a link to another machine: status %d, stderr %q", status, errOut)
	}
	if got := slurp(filepath.Join(bin, "wl-copy.in")); got != "" {
		t.Errorf("a link to another machine put %q on the clipboard", got)
	}
	note = slurp(filepath.Join(bin, "notify-send.args"))
	if !strings.HasSuffix(note, "--\nCouldn't copy\ntwo words.md is on another machine (elsewhere)\n") {
		t.Errorf("notification: %q", note)
	}
}

func TestHyperlinkModes(t *testing.T) {
	cfg := isolate(t)
	dir := realDir(t)
	write(t, filepath.Join(dir, "two words.md"), "hi\n")
	t.Chdir(dir)
	open := "\x1b]8;;file://" + dir + "/two%20words.md\x1b\\"
	copyLink := "\x1b]8;;" + clip.Link(filepath.Join(dir, "two words.md")) + "\x1b\\"
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-1"}, ""},
		{[]string{"-1", "--hyperlink"}, open},
		{[]string{"-1", "--hyperlink=open"}, open},
		{[]string{"-1", "--hyperlink=true"}, open},
		{[]string{"-1", "--hyperlink=copy"}, copyLink},
		{[]string{"--hyperlink", "copy", "-1"}, copyLink},
		{[]string{"-1", "--hyperlink", "--", "two words.md"}, open},
		{[]string{"-1", "--hyperlink=off"}, ""},
	} {
		out, errOut, status := glint(c.args...)
		if status != 0 {
			t.Errorf("glint %v: status %d, stderr %q", c.args, status, errOut)
		}
		if c.want == "" && strings.Contains(out, "\x1b]8") || !strings.Contains(out, c.want) {
			t.Errorf("glint %v printed %q, want link %q", c.args, out, c.want)
		}
	}
	if _, errOut, status := glint("-1", "--hyperlink=paste"); status != 2 || !strings.Contains(errOut, "invalid --hyperlink") {
		t.Errorf("--hyperlink=paste: status %d, stderr %q", status, errOut)
	}

	// the config file sets the default, the flag turns it off for one run;
	// `hyperlink = true` is how the option was written while it was on/off
	for _, c := range []struct {
		config string
		args   []string
		want   string
	}{
		{"hyperlink = true\n", []string{"-1"}, open},
		{"hyperlink = \"copy\"\n", []string{"-1"}, copyLink},
		{"hyperlink = \"copy\"\n", []string{"-1", "--hyperlink=off"}, ""},
		{"hyperlink = \"copy\"\n", []string{"-1", "--hyperlink=open"}, open},
	} {
		write(t, filepath.Join(cfg, "glint", "config.toml"), c.config)
		out, _, _ := glint(c.args...)
		if c.want == "" && strings.Contains(out, "\x1b]8") || !strings.Contains(out, c.want) {
			t.Errorf("config %q, glint %v printed %q, want link %q", c.config, c.args, out, c.want)
		}
	}
}

func TestInitCopyRegistersTheLinkHandler(t *testing.T) {
	bin := desktop(t, "xdg-mime", "update-desktop-database")
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // running it again replaces the entry
		out, errOut, status := glint("--init", "copy")
		if status != 0 || errOut != "" {
			t.Fatalf("status %d, stderr %q", status, errOut)
		}
		entry := slurp(filepath.Join(data, "applications", "glint-copy.desktop"))
		for _, line := range []string{
			"Exec=" + desktopArg(exe) + " --no-config --copy %u\n",
			"MimeType=x-scheme-handler/glint-copy;\n",
			"NoDisplay=true\n",
		} {
			if !strings.Contains(entry, line) {
				t.Errorf("the desktop entry lacks %q:\n%s", line, entry)
			}
		}
		if got, want := slurp(filepath.Join(bin, "xdg-mime.args")), "default\nglint-copy.desktop\nx-scheme-handler/glint-copy\n"; got != want {
			t.Errorf("xdg-mime got %q, want %q", got, want)
		}
		if !strings.Contains(out, `hyperlink = "copy"`) {
			t.Errorf("the output doesn't say how to turn the links on:\n%s", out)
		}
	}
}

func TestDesktopArg(t *testing.T) {
	for in, want := range map[string]string{
		"/home/me/go/bin/glint":   "/home/me/go/bin/glint",
		"/home/me/my tools/glint": `"/home/me/my tools/glint"`,
		`/opt/a"b$c%d\e/glint`:    `"/opt/a\\"b\\$c%%d\\\\e/glint"`,
	} {
		if got := desktopArg(in); got != want {
			t.Errorf("desktopArg(%q) = %s, want %s", in, got, want)
		}
	}
}

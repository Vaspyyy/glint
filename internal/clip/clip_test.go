//go:build unix

package clip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stub puts a fake clipboard tool on an otherwise empty PATH. It records
// its arguments and its input in dir.
func stub(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

const record = `echo "$@" > "$0.args"; /bin/cat > "$0.in"` + "\n"

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLinkComesBackAsTheSameFile(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"README.md", "two words.txt", "50%.txt", "a#b?c.txt", "ünï.txt", "q\"uote.txt"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		link := Link(path)
		if !strings.HasPrefix(link, Scheme+"://") {
			t.Errorf("Link(%q) = %q: wants the %s:// prefix terminals match on", path, link, Scheme)
		}
		got, isLink, err := Resolve(link)
		if err != nil || got != path || !isLink {
			t.Errorf("Resolve(%q) = %q, %v, %v; want %q, true, nil", link, got, isLink, err, path)
		}
	}
}

func TestResolve(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir()) // as the working directory reports it
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, c := range []struct {
		arg, want string
		link      bool
		errHas    string
	}{
		{"a.txt", file, false, ""},
		{file, file, false, ""},
		{"gone.txt", "", false, "no such file"},
		{Scheme + "://" + file, file, true, ""},
		{Scheme + "://localhost" + file, file, true, ""},
		{Scheme + "://" + dir + "/sub/../a.txt", file, true, ""},
		{Scheme + "://elsewhere" + file, "", true, "another machine (elsewhere)"},
		{Scheme + "://" + dir + "/gone.txt", "", true, "no such file"},
		{Scheme + ":a.txt", "", true, "not a link to a file"},
		{Scheme + "://", "", true, "not a link to a file"},
	} {
		got, link, err := Resolve(c.arg)
		if got != c.want || link != c.link || (err == nil) != (c.errHas == "") ||
			(err != nil && !strings.Contains(err.Error(), c.errHas)) {
			t.Errorf("Resolve(%q) = %q, %v, %v; want %q, %v, error with %q", c.arg, got, link, err, c.want, c.link, c.errHas)
		}
	}
}

func TestCopyHandsTheFilesToTheClipboardTool(t *testing.T) {
	paths := []string{"/tmp/a b.txt", "/tmp/c.png"}
	const list = "file:///tmp/a%20b.txt\r\nfile:///tmp/c.png\r\n"
	for _, c := range []struct{ tool, env, args string }{
		{"wl-copy", "WAYLAND_DISPLAY", "--type text/uri-list\n"},
		{"xclip", "DISPLAY", "-selection clipboard -t text/uri-list\n"},
	} {
		bin := t.TempDir()
		t.Setenv("PATH", bin)
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("DISPLAY", "")
		t.Setenv(c.env, "x")
		stub(t, bin, c.tool, record)
		if err := Copy(paths); err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		tool := filepath.Join(bin, c.tool)
		if got := read(t, tool+".args"); got != c.args {
			t.Errorf("%s got arguments %q, want %q", c.tool, got, c.args)
		}
		if got := read(t, tool+".in"); got != list {
			t.Errorf("%s got %q, want %q", c.tool, got, list)
		}
	}
}

// The real tools leave a process behind that keeps the clipboard, with the
// files it was started with still open. Copy must not wait for it.
func TestCopyDoesNotWaitForTheClipboardHolder(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("WAYLAND_DISPLAY", "x")
	stub(t, bin, "wl-copy", "/bin/cat > /dev/null; /bin/sleep 8 &\n")
	done := make(chan error, 1)
	go func() { done <- Copy([]string{"/tmp/a"}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-timeout(t):
		t.Fatal("Copy waited for the process that holds the clipboard")
	}
}

func TestCopyReportsWhatTheToolSaid(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("WAYLAND_DISPLAY", "x")
	t.Setenv("DISPLAY", "")
	stub(t, bin, "wl-copy", "echo 'Failed to connect to a Wayland server' >&2; exit 1\n")
	err := Copy([]string{"/tmp/a"})
	if err == nil || err.Error() != "wl-copy: Failed to connect to a Wayland server" {
		t.Errorf("got %v", err)
	}

	os.Remove(filepath.Join(bin, "wl-copy"))
	if err := Copy([]string{"/tmp/a"}); err == nil || !strings.Contains(err.Error(), "wl-clipboard") {
		t.Errorf("without wl-copy: got %v, want a hint to install wl-clipboard", err)
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	if err := Copy([]string{"/tmp/a"}); err == nil || !strings.Contains(err.Error(), "no desktop session") {
		t.Errorf("without a session: got %v", err)
	}
}

func timeout(t *testing.T) <-chan struct{} {
	c := make(chan struct{})
	timer := time.AfterFunc(4*time.Second, func() { close(c) })
	t.Cleanup(func() { timer.Stop() })
	return c
}

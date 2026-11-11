// Package clip puts files on the desktop clipboard the way a file manager's
// Copy does, so they can be pasted into a file manager, a chat or an upload
// field. It also defines the glint-copy: links that a listing prints with
// --hyperlink=copy and that `glint --copy` takes back when one is clicked.
package clip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Scheme is the URL scheme of a link that copies the file it names.
const Scheme = "glint-copy"

var hostname = sync.OnceValue(func() string {
	h, _ := os.Hostname()
	return h
})

// Link returns the glint-copy: URL for the file at abs. It names this
// machine, so a link printed over SSH is not mistaken for a local file.
func Link(abs string) string {
	u := url.URL{Scheme: Scheme, Host: hostname(), Path: abs}
	return u.String()
}

// Resolve turns an argument of `glint --copy`, a path or a glint-copy: URL,
// into the absolute path of a file that exists. link reports whether arg
// was a URL, which is what a click on a link passes.
func Resolve(arg string) (path string, link bool, err error) {
	if u, perr := url.Parse(arg); perr == nil && u.Scheme == Scheme {
		link = true
		if u.Opaque != "" || !filepath.IsAbs(u.Path) {
			return "", link, fmt.Errorf("%s: not a link to a file", arg)
		}
		if h := u.Hostname(); h != "" && h != "localhost" && !strings.EqualFold(h, hostname()) {
			return "", link, fmt.Errorf("%s is on another machine (%s)", filepath.Base(u.Path), h)
		}
		path = filepath.Clean(u.Path)
	} else if path, err = filepath.Abs(arg); err != nil {
		return "", link, err
	}
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", link, fmt.Errorf("%s: no such file", path)
		}
		return "", link, err
	}
	return path, link, nil
}

// URIList is paths in the clipboard's text/uri-list format.
func URIList(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		u := url.URL{Scheme: "file", Path: p}
		b.WriteString(u.String())
		b.WriteString("\r\n")
	}
	return b.String()
}

// Copy puts the files at paths (absolute) on the clipboard. It needs
// wl-copy on Wayland or xclip on X11: both keep serving the clipboard in
// the background after glint has exited.
func Copy(paths []string) error {
	var cmd *exec.Cmd
	wayland, x11 := os.Getenv("WAYLAND_DISPLAY") != "", os.Getenv("DISPLAY") != ""
	if tool, err := exec.LookPath("wl-copy"); err == nil && wayland {
		cmd = exec.Command(tool, "--type", "text/uri-list")
	} else if tool, err := exec.LookPath("xclip"); err == nil && x11 {
		cmd = exec.Command(tool, "-selection", "clipboard", "-t", "text/uri-list")
	} else {
		switch {
		case wayland:
			return errors.New("copying files needs wl-copy (install wl-clipboard)")
		case x11:
			return errors.New("copying files needs xclip")
		}
		return errors.New("no desktop session to copy to (neither WAYLAND_DISPLAY nor DISPLAY is set)")
	}
	cmd.Stdin = strings.NewReader(URIList(paths))
	// The tool leaves a process behind that holds the clipboard and inherits
	// these files. Go would wait for an ordinary writer until that process
	// exits, so stderr is a pipe glint reads itself, and only after a failure.
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer r.Close()
	cmd.Stderr = w
	err = cmd.Run()
	w.Close()
	if err != nil {
		name := filepath.Base(cmd.Path)
		r.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, _ := io.ReadAll(io.LimitReader(r, 2048))
		if s := strings.TrimSpace(string(msg)); s != "" {
			return fmt.Errorf("%s: %s", name, s)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Describe names what was copied: the file's name, or how many files.
func Describe(paths []string) string {
	if len(paths) == 1 {
		return filepath.Base(paths[0])
	}
	return fmt.Sprintf("%d files", len(paths))
}

// Notify shows a short desktop notification. A click on a link has no
// terminal to answer in, so this is where its result goes. Without
// notify-send, or without anything showing notifications, it does nothing.
func Notify(summary, body string) {
	tool, err := exec.LookPath("notify-send")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// the body may be shown as markup
	body = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(body)
	exec.CommandContext(ctx, tool, "-a", "glint", "-i", "edit-copy", "-t", "2500",
		"-h", "int:transient:1", "--", summary, body).Run()
}

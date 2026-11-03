// Package peek describes what's inside files and folders in a few words.
package peek

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"debug/elf"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/img"
	"github.com/Vaspyyy/glint/internal/project"
	"github.com/Vaspyyy/glint/internal/theme"
)

// Compute fills e.Peek for every entry, in parallel.
func Compute(es []*fsx.Entry) {
	fsx.ParallelMin(len(es), 4, func(i int) {
		if es[i].Peek == nil {
			es[i].Peek = Of(es[i])
		}
	})
}

func plain(text string) *fsx.Peek { return &fsx.Peek{Text: text, Color: theme.Muted} }

func Of(e *fsx.Entry) *fsx.Peek {
	switch {
	case e.Broken:
		return &fsx.Peek{Text: "broken link", Color: theme.Red}
	case e.Name == "." || e.Name == "..":
		return nil
	case e.IsDir:
		return dir(e)
	case !e.Mode.IsRegular() && !e.IsLink:
		return nil
	case e.Size == 0:
		return plain("empty")
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Name), "."))
	lower := strings.ToLower(e.Name)
	switch {
	case img.Supported(ext):
		if w, h, err := img.Dimensions(e.Path); err == nil {
			return &fsx.Peek{Text: fmt.Sprintf("%d×%d", w, h), Color: theme.Pink}
		}
	case media[ext]:
		if s := probe(e.Path); s != "" {
			return &fsx.Peek{Text: s, Color: theme.Purple}
		}
	case ext == "zip" || ext == "jar" || ext == "apk" || ext == "whl" || ext == "docx" || ext == "xlsx" || ext == "epub":
		if r, err := zip.OpenReader(e.Path); err == nil {
			n := len(r.File)
			r.Close()
			return &fsx.Peek{Text: plural(n, "file"), Color: theme.Red}
		}
	case ext == "tar" || ext == "tgz" || strings.HasSuffix(lower, ".tar.gz"):
		if n, err := tarCount(e.Path, ext != "tar"); err == nil {
			return &fsx.Peek{Text: plural(n, "file"), Color: theme.Red}
		}
	case ext == "pdf":
		if n := pdfPages(e.Path); n > 0 {
			return &fsx.Peek{Text: plural(n, "page"), Color: theme.Red}
		}
	}
	return content(e)
}

var media = map[string]bool{"mp3": true, "flac": true, "wav": true, "ogg": true, "m4a": true, "opus": true, "aac": true,
	"mp4": true, "mkv": true, "webm": true, "mov": true, "avi": true, "m4v": true, "wmv": true, "flv": true}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func dir(e *fsx.Entry) *fsx.Peek {
	names, err := project.Names(e.Path)
	if err != nil {
		return &fsx.Peek{Text: "no access", Color: theme.Red}
	}
	if p := project.Detect(e.Path, names); p != nil {
		return &fsx.Peek{Glyph: p.Glyph, Text: p.Label(), Color: p.Color}
	}
	if len(names) == 0 {
		return plain("empty")
	}
	return plain(plural(len(names), "item"))
}

const sniffSize = 8 << 10

func content(e *fsx.Entry) *fsx.Peek {
	f, err := os.Open(e.Path)
	if err != nil {
		return &fsx.Peek{Text: "no access", Color: theme.Red}
	}
	defer f.Close()
	head := make([]byte, sniffSize)
	n, _ := io.ReadFull(f, head)
	head = head[:n]

	if bytes.HasPrefix(head, []byte("\x7fELF")) {
		return elfInfo(e.Path)
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return plain("binary")
	}
	if e.Size > 64<<20 {
		return plain("text")
	}
	f.Seek(0, 0)
	lines := 0
	br := bufio.NewReaderSize(f, 64<<10)
	buf := make([]byte, 64<<10)
	var last byte
	for {
		m, err := br.Read(buf)
		lines += bytes.Count(buf[:m], []byte{'\n'})
		if m > 0 {
			last = buf[m-1]
		}
		if err != nil {
			break
		}
	}
	if last != '\n' {
		lines++
	}
	text := plural(lines, "line")
	if bytes.HasPrefix(head, []byte("#!")) {
		shebang, _, _ := bytes.Cut(head[2:], []byte("\n"))
		fields := strings.Fields(string(shebang))
		if len(fields) > 0 {
			interp := filepath.Base(fields[0])
			if interp == "env" && len(fields) > 1 {
				interp = fields[len(fields)-1]
			}
			text = interp + " script · " + text
		}
	}
	return plain(text)
}

func elfInfo(path string) *fsx.Peek {
	f, err := elf.Open(path)
	if err != nil {
		return plain("ELF binary")
	}
	defer f.Close()
	kind := "binary"
	switch f.Type {
	case elf.ET_DYN:
		kind = "executable"
		if f.Section(".interp") == nil {
			kind = "shared lib"
		}
	case elf.ET_EXEC:
		kind = "executable"
	case elf.ET_REL:
		kind = "object"
	}
	arch := strings.TrimPrefix(f.Machine.String(), "EM_")
	arch = strings.ToLower(strings.ReplaceAll(arch, "_", "-"))
	if arch == "x86-64" {
		arch = "x86_64"
	}
	static := ""
	if kind == "executable" && f.Section(".interp") == nil {
		static = " · static"
	}
	return &fsx.Peek{Text: "ELF " + arch + " " + kind + static, Color: theme.Green}
}

func tarCount(path string, gz bool) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if st, _ := f.Stat(); st != nil && st.Size() > 512<<20 {
		return 0, fmt.Errorf("too big")
	}
	var r io.Reader = f
	if gz {
		zr, err := gzip.NewReader(f)
		if err != nil {
			return 0, err
		}
		defer zr.Close()
		r = zr
	}
	tr := tar.NewReader(r)
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		if h.Typeflag != tar.TypeDir {
			n++
		}
	}
}

var pageRe = regexp.MustCompile(`/Type\s*/Page[^s]`)

func pdfPages(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		return 0
	}
	return len(pageRe.FindAllIndex(b, -1))
}

var (
	ffprobeOnce sync.Once
	ffprobePath string
)

// probe asks ffprobe (if installed) for duration and video resolution.
func probe(path string) string {
	ffprobeOnce.Do(func() { ffprobePath, _ = exec.LookPath("ffprobe") })
	if ffprobePath == "" {
		return ""
	}
	out, err := exec.Command(ffprobePath, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration", "-of", "default=nw=1", path).Output()
	if err != nil {
		return ""
	}
	var w, h string
	var dur float64
	for _, line := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch k {
		case "width":
			w = v
		case "height":
			h = v
		case "duration":
			dur, _ = strconv.ParseFloat(v, 64)
		}
	}
	var parts []string
	if dur > 0 {
		parts = append(parts, Duration(dur))
	}
	if w != "" && h != "" {
		parts = append(parts, w+"×"+h)
	}
	return strings.Join(parts, " · ")
}

func Duration(sec float64) string {
	s := int(sec + 0.5)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

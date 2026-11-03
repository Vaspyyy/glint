// Package icons maps files to Nerd Font glyphs and semantic kinds.
package icons

import (
	"io/fs"
	"strings"

	"github.com/Vaspyyy/glint/internal/theme"
)

type Kind int

const (
	File Kind = iota
	Dir
	Exec
	Link
	Broken
	Image
	Video
	Audio
	Archive
	Document
	Code
	Config
	Temp
	Special
)

var kindNames = [...]string{"file", "dir", "exec", "link", "broken", "image", "video", "audio", "archive", "document", "code", "config", "temp", "special"}

func (k Kind) String() string { return kindNames[k] }

type Icon struct {
	Glyph string
	Color theme.Slot
}

type Info struct {
	Name                       string
	Mode                       fs.FileMode
	IsDir, IsLink, Broken, Exe bool
}

var (
	iconDir     = Icon{"\ue5ff", theme.Blue}
	iconFile    = Icon{"\uf15b", theme.Subtle}
	iconExec    = Icon{"\uf489", theme.Green}
	iconLink    = Icon{"\uf481", theme.Cyan}
	iconLinkDir = Icon{"\uf482", theme.Cyan}
	iconBroken  = Icon{"\uf127", theme.Red}
	iconSpecial = Icon{"\uf0a0", theme.Yellow}
	iconTemp    = Icon{"\uf15b", theme.Faint}
)

func Lookup(in Info) (Icon, Kind) {
	lower := strings.ToLower(in.Name)
	switch {
	case in.Broken:
		return iconBroken, Broken
	case in.IsDir:
		if in.IsLink {
			return iconLinkDir, Dir
		}
		if ic, ok := dirs[lower]; ok {
			return ic, Dir
		}
		return iconDir, Dir
	case in.Mode&(fs.ModeNamedPipe|fs.ModeSocket|fs.ModeDevice|fs.ModeCharDevice) != 0:
		return iconSpecial, Special
	}

	x, found := names[lower]
	if !found {
		if ext := extOf(lower); ext != "" {
			x, found = exts[ext]
		}
	}
	if !found && strings.HasSuffix(lower, "~") {
		x, found = def{iconTemp, Temp}, true
	}

	switch {
	case in.IsLink:
		return iconLink, Link
	case in.Exe:
		if found {
			return x.Icon, Exec
		}
		return iconExec, Exec
	case found:
		return x.Icon, x.Kind
	}
	return iconFile, File
}

func extOf(lower string) string {
	i := strings.LastIndexByte(lower, '.')
	if i <= 0 {
		return ""
	}
	return lower[i+1:]
}

type def struct {
	Icon
	Kind Kind
}

func d(glyph string, c theme.Slot, k Kind) def { return def{Icon{glyph, c}, k} }

var dirs = map[string]Icon{
	".git":         {"\ue5fb", theme.Orange},
	".github":      {"\ue5fd", theme.Blue},
	".config":      {"\ue5fc", theme.Blue},
	"node_modules": {"\ue5fa", theme.Green},
	".vscode":      {"\ue70c", theme.Blue},
	"desktop":      {"\uf108", theme.Blue},
	"documents":    {"\uf02d", theme.Blue},
	"downloads":    {"\uf019", theme.Blue},
	"music":        {"\uf001", theme.Blue},
	"pictures":     {"\uf03e", theme.Blue},
	"videos":       {"\uf03d", theme.Blue},
}

var names = map[string]def{
	"makefile":           d("\ue779", theme.Orange, Config),
	"cmakelists.txt":     d("\ue779", theme.Orange, Config),
	"justfile":           d("\uf489", theme.Orange, Config),
	"dockerfile":         d("\uf308", theme.Blue, Config),
	"containerfile":      d("\uf308", theme.Blue, Config),
	"docker-compose.yml": d("\uf308", theme.Blue, Config),
	"compose.yaml":       d("\uf308", theme.Blue, Config),
	".gitignore":         d("\uf1d3", theme.Orange, Config),
	".gitattributes":     d("\uf1d3", theme.Orange, Config),
	".gitmodules":        d("\uf1d3", theme.Orange, Config),
	"license":            d("\uf0e3", theme.Yellow, Document),
	"license.md":         d("\uf0e3", theme.Yellow, Document),
	"license.txt":        d("\uf0e3", theme.Yellow, Document),
	"copying":            d("\uf0e3", theme.Yellow, Document),
	"readme":             d("\uf05a", theme.Yellow, Document),
	"readme.md":          d("\uf05a", theme.Yellow, Document),
	"readme.txt":         d("\uf05a", theme.Yellow, Document),
	"package.json":       d("\ue71e", theme.Red, Config),
	"package-lock.json":  d("\ue71e", theme.Faint, Temp),
	"pnpm-lock.yaml":     d("\ue71e", theme.Faint, Temp),
	"yarn.lock":          d("\ue71e", theme.Faint, Temp),
	"cargo.toml":         d("\ue7a8", theme.Orange, Config),
	"cargo.lock":         d("\ue7a8", theme.Faint, Temp),
	"go.mod":             d("\ue627", theme.Purple, Config),
	"go.sum":             d("\ue627", theme.Faint, Temp),
	"flake.nix":          d("\uf313", theme.Blue, Config),
	"flake.lock":         d("\uf313", theme.Faint, Temp),
	".env":               d("\uf084", theme.Yellow, Config),
	".editorconfig":      d("\ue615", theme.Muted, Config),
	".bashrc":            d("\uf489", theme.Green, Config),
	".zshrc":             d("\uf489", theme.Green, Config),
	".profile":           d("\uf489", theme.Green, Config),
}

var exts = func() map[string]def {
	m := map[string]def{}
	add := func(x def, list ...string) {
		for _, e := range list {
			m[e] = x
		}
	}
	// code
	add(d("\ue627", theme.Cyan, Code), "go")
	add(d("\ue7a8", theme.Orange, Code), "rs")
	add(d("\ue73c", theme.Yellow, Code), "py", "pyi")
	add(d("\ue74e", theme.Yellow, Code), "js", "mjs", "cjs")
	add(d("\ue628", theme.Blue, Code), "ts", "mts", "cts")
	add(d("\ue7ba", theme.Cyan, Code), "jsx", "tsx")
	add(d("\ue738", theme.Red, Code), "java")
	add(d("\ue634", theme.Purple, Code), "kt", "kts")
	add(d("\ue61e", theme.Blue, Code), "c")
	add(d("\ue61d", theme.Blue, Code), "cpp", "cc", "cxx", "c++", "hpp", "hh")
	add(d("\uf0fd", theme.Purple, Code), "h")
	add(d("\U000f031b", theme.Purple, Code), "cs")
	add(d("\ue739", theme.Red, Code), "rb")
	add(d("\ue73d", theme.Purple, Code), "php")
	add(d("\ue620", theme.Blue, Code), "lua")
	add(d("\uf489", theme.Green, Code), "sh", "bash", "zsh", "fish", "nu")
	add(d("\ue755", theme.Orange, Code), "swift")
	add(d("\ue777", theme.Purple, Code), "hs")
	add(d("\ue62d", theme.Purple, Code), "ex", "exs")
	add(d("\ue6a9", theme.Orange, Code), "zig")
	add(d("\ue798", theme.Cyan, Code), "dart")
	add(d("\U000f0844", theme.Green, Code), "vue")
	add(d("\ue697", theme.Orange, Code), "svelte")
	add(d("\ue736", theme.Orange, Code), "html", "htm")
	add(d("\ue749", theme.Blue, Code), "css")
	add(d("\ue603", theme.Pink, Code), "scss", "sass")
	add(d("\ue62b", theme.Green, Code), "vim")
	add(d("\uf313", theme.Blue, Code), "nix")
	add(d("\uf1c0", theme.Yellow, Code), "sql")
	add(d("\ue73c", theme.Orange, Code), "ipynb")
	// config & data
	add(d("\ue60b", theme.Yellow, Config), "json", "jsonc", "json5")
	add(d("\ue615", theme.Purple, Config), "yaml", "yml")
	add(d("\ue615", theme.Orange, Config), "toml", "ini", "conf", "cfg", "env")
	add(d("\U000f05c0", theme.Orange, Config), "xml")
	add(d("\uf1c0", theme.Yellow, Config), "db", "sqlite", "sqlite3")
	add(d("\uf084", theme.Yellow, Config), "pem", "key", "crt", "pub", "gpg", "asc")
	add(d("\uf023", theme.Faint, Temp), "lock")
	add(d("\uf15b", theme.Faint, Temp), "bak", "tmp", "swp", "orig", "pyc", "o", "class")
	// documents
	add(d("\uf48a", theme.Blue, Document), "md", "markdown", "mdx")
	add(d("\uf15c", theme.Text, Document), "txt", "rst", "org")
	add(d("\uf15c", theme.Muted, Document), "log")
	add(d("\uf1c1", theme.Red, Document), "pdf")
	add(d("\uf1c2", theme.Blue, Document), "doc", "docx", "odt", "rtf")
	add(d("\uf1c3", theme.Green, Document), "xls", "xlsx", "ods", "csv", "tsv")
	add(d("\uf1c4", theme.Orange, Document), "ppt", "pptx", "odp")
	add(d("\uf02d", theme.Purple, Document), "epub", "mobi")
	add(d("\uf440", theme.Green, Document), "diff", "patch")
	// media
	add(d("\uf1c5", theme.Pink, Image), "png", "jpg", "jpeg", "gif", "bmp", "webp", "tiff", "ico", "heic", "avif", "psd", "xcf")
	add(d("\U000f0721", theme.Orange, Image), "svg")
	add(d("\uf03d", theme.Purple, Video), "mp4", "mkv", "webm", "mov", "avi", "flv", "m4v", "wmv")
	add(d("\uf001", theme.Teal, Audio), "mp3", "flac", "wav", "ogg", "m4a", "opus", "aac", "mid")
	add(d("\uf031", theme.Subtle, File), "ttf", "otf", "woff", "woff2")
	// archives & binaries
	add(d("\uf410", theme.Red, Archive), "zip", "tar", "gz", "tgz", "xz", "bz2", "zst", "7z", "rar", "lz4")
	add(d("\uf487", theme.Orange, Archive), "deb", "rpm", "pkg", "apk", "jar", "whl", "appimage", "flatpak")
	add(d("\uf0a0", theme.Muted, Archive), "iso", "img", "qcow2")
	add(d("\uf471", theme.Muted, File), "exe", "dll", "so", "a", "dylib", "wasm", "bin")
	return m
}()

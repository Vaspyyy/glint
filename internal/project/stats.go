package project

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Vaspyyy/glint/internal/fsx"
	"github.com/Vaspyyy/glint/internal/git"
	"github.com/Vaspyyy/glint/internal/theme"
)

type Lang struct {
	Name  string
	Color theme.Slot
	Lines int
	Files int
}

// Stats is everything a project card shows.
type Stats struct {
	Info   *Info
	Repo   *git.Repo
	Last   *git.Commit
	Langs  []Lang // sorted by lines, descending
	Lines  int
	Files  int
	Todos  int
	Capped bool // stopped early on a huge tree
}

var langs = map[string]Lang{
	".go": {Name: "Go", Color: theme.Cyan}, ".rs": {Name: "Rust", Color: theme.Orange},
	".py": {Name: "Python", Color: theme.Yellow}, ".js": {Name: "JavaScript", Color: theme.Yellow},
	".mjs": {Name: "JavaScript", Color: theme.Yellow}, ".cjs": {Name: "JavaScript", Color: theme.Yellow},
	".ts": {Name: "TypeScript", Color: theme.Blue}, ".tsx": {Name: "TSX", Color: theme.Cyan},
	".jsx": {Name: "JSX", Color: theme.Cyan}, ".java": {Name: "Java", Color: theme.Red},
	".kt": {Name: "Kotlin", Color: theme.Purple}, ".kts": {Name: "Kotlin", Color: theme.Purple},
	".c": {Name: "C", Color: theme.Blue}, ".h": {Name: "C", Color: theme.Blue},
	".cpp": {Name: "C++", Color: theme.Pink}, ".cc": {Name: "C++", Color: theme.Pink},
	".hpp": {Name: "C++", Color: theme.Pink}, ".cs": {Name: "C#", Color: theme.Purple},
	".rb": {Name: "Ruby", Color: theme.Red}, ".php": {Name: "PHP", Color: theme.Purple},
	".lua": {Name: "Lua", Color: theme.Blue}, ".sh": {Name: "Shell", Color: theme.Green},
	".bash": {Name: "Shell", Color: theme.Green}, ".fish": {Name: "Shell", Color: theme.Green},
	".swift": {Name: "Swift", Color: theme.Orange}, ".zig": {Name: "Zig", Color: theme.Orange},
	".dart": {Name: "Dart", Color: theme.Cyan}, ".vue": {Name: "Vue", Color: theme.Green},
	".svelte": {Name: "Svelte", Color: theme.Orange}, ".html": {Name: "HTML", Color: theme.Orange},
	".css": {Name: "CSS", Color: theme.Purple}, ".scss": {Name: "SCSS", Color: theme.Pink},
	".md": {Name: "Markdown", Color: theme.Subtle}, ".sql": {Name: "SQL", Color: theme.Yellow},
	".nix": {Name: "Nix", Color: theme.Blue}, ".gd": {Name: "GDScript", Color: theme.Blue},
	".ex": {Name: "Elixir", Color: theme.Purple}, ".exs": {Name: "Elixir", Color: theme.Purple},
	".hs": {Name: "Haskell", Color: theme.Purple}, ".toml": {Name: "TOML", Color: theme.Muted},
	".yaml": {Name: "YAML", Color: theme.Muted}, ".yml": {Name: "YAML", Color: theme.Muted},
	".json": {Name: "JSON", Color: theme.Muted}, ".txt": {Name: "Text", Color: theme.Faint},
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "target": true, "vendor": true,
	"dist": true, "build": true, ".venv": true, "venv": true, "__pycache__": true, ".next": true, ".cache": true}

const (
	maxFiles    = 20000
	maxFileSize = 2 << 20
)

// Collect gathers card stats for dir. repo may be nil.
func Collect(dir string, repo *git.Repo) *Stats {
	st := &Stats{Info: Detect(dir, nil), Repo: repo}
	var files []string
	if repo != nil {
		st.Last = repo.LastCommit()
		files = repo.TrackedFiles(dir)
	} else {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != dir && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if len(files) >= maxFiles {
				return filepath.SkipAll
			}
			files = append(files, p)
			return nil
		})
	}
	if len(files) > maxFiles {
		files, st.Capped = files[:maxFiles], true
	}

	type res struct {
		ext          string
		lines, todos int
	}
	results := make([]res, len(files))
	fsx.ParallelMin(len(files), 16, func(i int) {
		ext := strings.ToLower(filepath.Ext(files[i]))
		if _, ok := langs[ext]; !ok {
			return
		}
		info, err := os.Lstat(files[i])
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileSize {
			return
		}
		b, err := fsx.ReadFile(files[i], 0)
		if err != nil || bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
			return
		}
		n := bytes.Count(b, []byte{'\n'})
		if len(b) > 0 && b[len(b)-1] != '\n' {
			n++
		}
		todos := bytes.Count(b, []byte("TODO")) + bytes.Count(b, []byte("FIXME")) + bytes.Count(b, []byte("HACK"))
		results[i] = res{ext, n, todos}
	})

	byLang := map[string]*Lang{}
	for _, r := range results {
		if r.ext == "" {
			continue
		}
		l := langs[r.ext]
		agg, ok := byLang[l.Name]
		if !ok {
			agg = &Lang{Name: l.Name, Color: l.Color}
			byLang[l.Name] = agg
		}
		agg.Lines += r.lines
		agg.Files++
		st.Lines += r.lines
		st.Files++
		st.Todos += r.todos
	}
	for _, l := range byLang {
		st.Langs = append(st.Langs, *l)
	}
	sort.Slice(st.Langs, func(i, j int) bool { return st.Langs[i].Lines > st.Langs[j].Lines })
	return st
}

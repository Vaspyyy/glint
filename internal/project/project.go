// Package project recognises what kind of project a directory is.
package project

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Vaspyyy/glint/internal/theme"
)

type Info struct {
	Stack  string // "Go", "Rust", "Next.js", ...
	Name   string // module / crate / package name if known
	Detail string // version, framework hints
	Glyph  string
	Color  theme.Slot
}

func (i *Info) Label() string {
	parts := []string{i.Stack}
	if i.Name != "" {
		parts = append(parts, i.Name)
	}
	if i.Detail != "" {
		parts = append(parts, i.Detail)
	}
	return strings.Join(parts, " · ")
}

// Names reads a directory's entry names (one syscall, no stat).
func Names(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Readdirnames(-1)
}

// Detect inspects marker files. names may be nil, in which case it is read.
func Detect(dir string, names []string) *Info {
	if names == nil {
		var err error
		if names, err = Names(dir); err != nil {
			return nil
		}
	}
	has := make(map[string]bool, len(names))
	ext := map[string]bool{}
	for _, n := range names {
		has[n] = true
		if i := strings.LastIndexByte(n, '.'); i > 0 {
			ext[strings.ToLower(n[i:])] = true
		}
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return string(b)
	}

	switch {
	case has["go.mod"]:
		in := &Info{Stack: "Go", Glyph: "\ue627", Color: theme.Cyan}
		for _, line := range strings.Split(read("go.mod"), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == "module" {
				in.Name = f[1]
			}
			if len(f) == 2 && f[0] == "go" {
				in.Detail = "go " + f[1]
			}
		}
		return in
	case has["Cargo.toml"]:
		in := &Info{Stack: "Rust", Glyph: "\ue7a8", Color: theme.Orange}
		in.Name = tomlKey(read("Cargo.toml"), "package", "name")
		if in.Name == "" && strings.Contains(read("Cargo.toml"), "[workspace]") {
			in.Detail = "workspace"
		}
		return in
	case has["package.json"]:
		return node(dir, has, read("package.json"))
	case has["pyproject.toml"] || has["setup.py"] || has["requirements.txt"] || has["Pipfile"]:
		in := &Info{Stack: "Python", Glyph: "\ue73c", Color: theme.Yellow}
		if has["pyproject.toml"] {
			py := read("pyproject.toml")
			in.Name = tomlKey(py, "project", "name")
			if in.Name == "" {
				in.Name = tomlKey(py, "tool.poetry", "name")
			}
		}
		return in
	case has["build.zig"]:
		return &Info{Stack: "Zig", Glyph: "\ue6a9", Color: theme.Orange}
	case has["project.godot"]:
		return &Info{Stack: "Godot", Name: iniValue(read("project.godot"), "config/name"), Glyph: "\uf11b", Color: theme.Blue}
	case ext[".uproject"]:
		return &Info{Stack: "Unreal", Glyph: "\uf11b", Color: theme.Subtle}
	case has["ProjectSettings"] && has["Assets"]:
		return &Info{Stack: "Unity", Glyph: "\ue721", Color: theme.Subtle}
	case has["pubspec.yaml"]:
		return &Info{Stack: "Flutter", Name: yamlKey(read("pubspec.yaml"), "name"), Glyph: "\ue798", Color: theme.Cyan}
	case has["build.gradle"] || has["build.gradle.kts"] || has["settings.gradle"] || has["settings.gradle.kts"]:
		if has["app"] {
			if _, err := os.Stat(filepath.Join(dir, "app", "src", "main", "AndroidManifest.xml")); err == nil {
				return &Info{Stack: "Android", Glyph: "\ue70e", Color: theme.Green}
			}
		}
		if has["build.gradle.kts"] || has["settings.gradle.kts"] {
			return &Info{Stack: "Kotlin", Detail: "gradle", Glyph: "\ue634", Color: theme.Purple}
		}
		return &Info{Stack: "Java", Detail: "gradle", Glyph: "\ue738", Color: theme.Red}
	case has["pom.xml"]:
		return &Info{Stack: "Java", Detail: "maven", Glyph: "\ue738", Color: theme.Red}
	case ext[".sln"] || ext[".csproj"] || ext[".fsproj"]:
		return &Info{Stack: "C#", Detail: ".NET", Glyph: "\U000f031b", Color: theme.Purple}
	case has["mix.exs"]:
		return &Info{Stack: "Elixir", Glyph: "\ue62d", Color: theme.Purple}
	case has["Gemfile"]:
		return &Info{Stack: "Ruby", Glyph: "\ue739", Color: theme.Red}
	case has["composer.json"]:
		return &Info{Stack: "PHP", Glyph: "\ue73d", Color: theme.Purple}
	case has["deno.json"] || has["deno.jsonc"]:
		return &Info{Stack: "Deno", Glyph: "\ue628", Color: theme.Text}
	case has["descriptor.mod"]:
		return &Info{Stack: "Paradox mod", Name: iniValue(read("descriptor.mod"), "name"), Glyph: "\uf024", Color: theme.Red}
	case has["CMakeLists.txt"]:
		return &Info{Stack: "C/C++", Detail: "cmake", Glyph: "\ue61d", Color: theme.Blue}
	case has["meson.build"]:
		return &Info{Stack: "C/C++", Detail: "meson", Glyph: "\ue61d", Color: theme.Blue}
	case has["flake.nix"] || has["default.nix"]:
		return &Info{Stack: "Nix", Glyph: "\uf313", Color: theme.Blue}
	case has["PKGBUILD"]:
		return &Info{Stack: "AUR package", Name: shellVar(read("PKGBUILD"), "pkgname"), Glyph: "\uf303", Color: theme.Cyan}
	case has["main.lua"] && has["conf.lua"]:
		return &Info{Stack: "LÖVE", Glyph: "\ue620", Color: theme.Pink}
	case has["index.html"] && (ext[".css"] || ext[".js"] || has["assets"] || has["css"] || has["js"]):
		return &Info{Stack: "Website", Glyph: "\ue736", Color: theme.Orange}
	case ext[".c"] || ext[".cpp"] || ext[".cc"] || ext[".cxx"]:
		stack := "C"
		if ext[".cpp"] || ext[".cc"] || ext[".cxx"] {
			stack = "C++"
		}
		detail := ""
		switch {
		case has["Makefile"] || has["makefile"]:
			detail = "make"
		case has["build.sh"]:
			detail = "build.sh"
		}
		return &Info{Stack: stack, Detail: detail, Glyph: "\ue61e", Color: theme.Blue}
	case has["Makefile"] || has["makefile"]:
		return &Info{Stack: "Make", Glyph: "\ue779", Color: theme.Orange}
	case has["Dockerfile"] || has["compose.yaml"] || has["docker-compose.yml"]:
		return &Info{Stack: "Docker", Glyph: "\uf308", Color: theme.Blue}
	case ext[".py"]:
		return &Info{Stack: "Python", Detail: "scripts", Glyph: "\ue73c", Color: theme.Yellow}
	case ext[".sh"] && len(names) < 30:
		return &Info{Stack: "Shell", Detail: "scripts", Glyph: "\uf489", Color: theme.Green}
	case has[".git"]:
		return &Info{Stack: "git repo", Glyph: "\ue702", Color: theme.Orange}
	}
	return nil
}

func node(dir string, has map[string]bool, raw string) *Info {
	var pkg struct {
		Name            string            `json:"name"`
		Deps            map[string]string `json:"dependencies"`
		DevDeps         map[string]string `json:"devDependencies"`
		PackageManager  string            `json:"packageManager"`
		Workspaces      json.RawMessage   `json:"workspaces"`
		Main            string            `json:"main"`
		ElectronVersion string            `json:"electronVersion"`
	}
	json.Unmarshal([]byte(raw), &pkg)
	dep := func(n string) bool {
		_, a := pkg.Deps[n]
		_, b := pkg.DevDeps[n]
		return a || b
	}
	in := &Info{Stack: "Node", Name: pkg.Name, Glyph: "\ue718", Color: theme.Green}
	ts := has["tsconfig.json"] || dep("typescript")
	switch {
	case dep("next"):
		in.Stack, in.Glyph, in.Color = "Next.js", "\ue7ba", theme.Text
	case dep("electron"):
		in.Stack, in.Glyph, in.Color = "Electron", "\ue62e", theme.Cyan
	case dep("@tauri-apps/api") || has["src-tauri"]:
		in.Stack, in.Glyph, in.Color = "Tauri (Rust + web)", "\ue7a8", theme.Orange
	case dep("react-native") || dep("expo"):
		in.Stack, in.Glyph, in.Color = "React Native", "\ue7ba", theme.Cyan
	case dep("@sveltejs/kit") || dep("svelte"):
		in.Stack, in.Glyph, in.Color = "Svelte", "\ue697", theme.Orange
	case dep("nuxt") || dep("vue"):
		in.Stack, in.Glyph, in.Color = "Vue", "\U000f0844", theme.Green
	case dep("@angular/core"):
		in.Stack, in.Glyph, in.Color = "Angular", "\ue753", theme.Red
	case dep("astro"):
		in.Stack, in.Glyph, in.Color = "Astro", "\ue6b3", theme.Orange
	case dep("react"):
		in.Stack, in.Glyph, in.Color = "React", "\ue7ba", theme.Cyan
	case dep("express") || dep("fastify") || dep("hono"):
		in.Stack = "Node server"
	case ts:
		in.Stack, in.Glyph, in.Color = "TypeScript", "\ue628", theme.Blue
	}
	if ts && in.Stack != "TypeScript" {
		in.Detail = "ts"
	}
	if dep("vite") {
		in.Detail = strings.TrimPrefix(in.Detail+" · vite", " · ")
	}
	if has["bun.lockb"] || has["bun.lock"] {
		in.Detail = strings.TrimPrefix(in.Detail+" · bun", " · ")
	}
	return in
}

func tomlKey(src, section, key string) string {
	cur := ""
	sc := bufio.NewScanner(strings.NewReader(src))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			cur = strings.Trim(line, "[] ")
			continue
		}
		if cur != section {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

func iniValue(src, key string) string {
	for _, line := range strings.Split(src, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

func yamlKey(src, key string) string {
	for _, line := range strings.Split(src, "\n") {
		if v, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

func shellVar(src, key string) string {
	for _, line := range strings.Split(src, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), key+"="); ok {
			return strings.Trim(strings.Trim(v, "()"), `"'`)
		}
	}
	return ""
}

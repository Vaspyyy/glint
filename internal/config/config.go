// Package config loads ~/.config/glint/config.toml.
//
// Every top-level key is a long flag name (theme = "nord", classify = true,
// ignore = ["*.tmp"]), and [themes.NAME] tables define custom palettes.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"

	"github.com/Vaspyyy/glint/internal/regfile"
	"github.com/Vaspyyy/glint/internal/theme"
)

func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "glint", "config.toml")
}

type Setter interface {
	Set(name, value string) error
}

// maxSize is how much of the config file in its usual place is read.
const maxSize = 4 << 20

// Load applies flag defaults from the config file and registers custom
// themes. A missing file is not an error. The file in its usual place is
// read on every run, so it has to be a regular file; one named with
// --config (named) is read as it is, which can be a pipe: --config <(…).
func Load(path string, named bool, flags Setter) error {
	var data []byte
	var err error
	if named {
		data, err = os.ReadFile(path)
	} else {
		data, err = regfile.Read(path, maxSize)
	}
	if os.IsNotExist(err) {
		return nil
	}
	raw := map[string]any{}
	if err == nil {
		_, err = toml.Decode(string(data), &raw)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if themes, ok := raw["themes"].(map[string]any); ok {
		if err := loadThemes(themes); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		delete(raw, "themes")
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vals := []any{raw[k]}
		if list, ok := raw[k].([]any); ok {
			vals = list
		}
		for _, v := range vals {
			if b, ok := v.(bool); ok && !b {
				continue // `foo = false` simply leaves the default alone
			}
			if err := flags.Set(k, fmt.Sprint(v)); err != nil {
				return fmt.Errorf("%s: %s: %w", path, k, err)
			}
		}
	}
	return nil
}

func loadThemes(themes map[string]any) error {
	for name, v := range themes {
		tbl, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("themes.%s must be a table", name)
		}
		base := theme.Palettes["catppuccin"]
		if ext, ok := tbl["extends"].(string); ok {
			p, err := theme.Lookup(ext)
			if err != nil {
				return fmt.Errorf("themes.%s: %w", name, err)
			}
			base = p
		}
		for k, c := range tbl {
			if k == "extends" {
				continue
			}
			slot, ok := theme.ParseSlot(k)
			if !ok {
				return fmt.Errorf("themes.%s: unknown color %q", name, k)
			}
			base[slot] = fmt.Sprint(c)
		}
		theme.Palettes[name] = base
	}
	return nil
}

const Sample = `# glint config — every key is a long flag name (see glint --help)

# theme = "tokyonight"      # catppuccin, tokyonight, gruvbox, nord, wal, or your own below
# icons = "always"
# classify = true
# hyperlink = true
# peek = true
# group-dirs = "first"
# time-style = "relative"
# ignore = ["*.pyc", "__pycache__"]

# [themes.midnight]
# extends = "catppuccin"
# blue = "#7aa2f7"
# purple = "#bb9af7"
`

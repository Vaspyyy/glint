// Package theme holds color palettes and a tiny, allocation-light ANSI styler.
//
// lipgloss is lovely for layout, but an ls renders thousands of tiny styled
// cells, so styles here are precomputed SGR prefixes instead.
package theme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"

	"github.com/Vaspyyy/glint/internal/regfile"
)

type Slot int

const (
	Text Slot = iota
	Subtle
	Muted
	Faint
	Red
	Orange
	Yellow
	Green
	Teal
	Cyan
	Blue
	Purple
	Pink
	Base    // terminal background, used for text on colored blocks
	Surface // slightly lifted background, used for highlights
	NumSlots
)

var slotNames = [...]string{"text", "subtle", "muted", "faint", "red", "orange", "yellow", "green", "teal", "cyan", "blue", "purple", "pink", "base", "surface"}

func ParseSlot(name string) (Slot, bool) {
	for i, n := range slotNames {
		if n == strings.ToLower(name) {
			return Slot(i), true
		}
	}
	return 0, false
}

// Accents are the colorful slots, handy for cycling (treemaps, language bars).
var Accents = []Slot{Blue, Purple, Pink, Red, Orange, Yellow, Green, Teal, Cyan}

type Palette [NumSlots]string

var Palettes = map[string]Palette{
	"catppuccin": {
		Text: "#cdd6f4", Subtle: "#bac2de", Muted: "#7f849c", Faint: "#585b70",
		Red: "#f38ba8", Orange: "#fab387", Yellow: "#f9e2af", Green: "#a6e3a1",
		Teal: "#94e2d5", Cyan: "#89dceb", Blue: "#89b4fa", Purple: "#cba6f7", Pink: "#f5c2e7",
		Base: "#1e1e2e", Surface: "#313244",
	},
	"tokyonight": {
		Text: "#c0caf5", Subtle: "#a9b1d6", Muted: "#737aa2", Faint: "#565f89",
		Red: "#f7768e", Orange: "#ff9e64", Yellow: "#e0af68", Green: "#9ece6a",
		Teal: "#73daca", Cyan: "#7dcfff", Blue: "#7aa2f7", Purple: "#bb9af7", Pink: "#ff7eb6",
		Base: "#1a1b26", Surface: "#292e42",
	},
	"gruvbox": {
		Text: "#ebdbb2", Subtle: "#d5c4a1", Muted: "#a89984", Faint: "#665c54",
		Red: "#fb4934", Orange: "#fe8019", Yellow: "#fabd2f", Green: "#b8bb26",
		Teal: "#8ec07c", Cyan: "#83a598", Blue: "#83a598", Purple: "#d3869b", Pink: "#d3869b",
		Base: "#282828", Surface: "#3c3836",
	},
	"nord": {
		Text: "#eceff4", Subtle: "#d8dee9", Muted: "#7b88a1", Faint: "#4c566a",
		Red: "#bf616a", Orange: "#d08770", Yellow: "#ebcb8b", Green: "#a3be8c",
		Teal: "#8fbcbb", Cyan: "#88c0d0", Blue: "#81a1c1", Purple: "#b48ead", Pink: "#c895bf",
		Base: "#2e3440", Surface: "#3b4252",
	},
}

func Names() []string {
	names := make([]string, 0, len(Palettes)+1)
	for n := range Palettes {
		names = append(names, n)
	}
	names = append(names, "wal")
	sort.Strings(names)
	return names
}

// Lookup resolves a theme name; "wal" reads pywal's generated colors.
func Lookup(name string) (Palette, error) {
	if name == "wal" {
		return walPalette()
	}
	if p, ok := Palettes[name]; ok {
		return p, nil
	}
	return Palette{}, fmt.Errorf("unknown theme %q (have %s)", name, strings.Join(Names(), ", "))
}

func walPalette() (Palette, error) {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	data, err := regfile.Read(filepath.Join(dir, "wal", "colors.json"), 1<<20)
	if err != nil {
		return Palette{}, fmt.Errorf("theme wal: %w (run pywal first)", err)
	}
	var w struct {
		Special map[string]string `json:"special"`
		Colors  map[string]string `json:"colors"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return Palette{}, fmt.Errorf("theme wal: %w", err)
	}
	c := func(n int) string { return w.Colors[fmt.Sprintf("color%d", n)] }
	p := Palette{
		Text: w.Special["foreground"], Subtle: c(7), Muted: c(8), Faint: c(8),
		Red: c(1), Orange: c(9), Yellow: c(3), Green: c(2), Teal: c(14), Cyan: c(6),
		Blue: c(4), Purple: c(5), Pink: c(13), Base: w.Special["background"], Surface: c(0),
	}
	if fg, err := colorful.Hex(p[Text]); err == nil {
		if bg, err := colorful.Hex(p[Base]); err == nil {
			p[Faint] = bg.BlendLab(fg, 0.35).Hex()
			p[Surface] = bg.BlendLab(fg, 0.12).Hex()
		}
	}
	return p, nil
}

// Style is a precomputed SGR sequence. The zero value renders text unchanged.
type Style struct {
	on  bool
	seq string
}

func (s Style) Render(t string) string {
	if s.seq == "" || t == "" {
		return t
	}
	return "\x1b[" + s.seq + "m" + t + "\x1b[0m"
}

// Seq is the raw escape sequence that turns this style on ("" when disabled).
func (s Style) Seq() string {
	if s.seq == "" {
		return ""
	}
	return "\x1b[" + s.seq + "m"
}

func (s Style) add(code string) Style {
	if !s.on || code == "" {
		return s
	}
	if s.seq == "" {
		s.seq = code
	} else {
		s.seq += ";" + code
	}
	return s
}

func (s Style) Bold() Style      { return s.add("1") }
func (s Style) Faint() Style     { return s.add("2") }
func (s Style) Italic() Style    { return s.add("3") }
func (s Style) Underline() Style { return s.add("4") }
func (s Style) Strike() Style    { return s.add("9") }

type Painter struct {
	profile termenv.Profile
	Pal     Palette
}

func NewPainter(profile termenv.Profile, pal Palette) *Painter {
	return &Painter{profile: profile, Pal: pal}
}

func (p *Painter) Enabled() bool            { return p.profile != termenv.Ascii }
func (p *Painter) Profile() termenv.Profile { return p.profile }
func (p *Painter) Plain() Style             { return Style{on: p.Enabled()} }

func (p *Painter) Fg(s Slot) Style { return p.Hex(p.Pal[s]) }

func (p *Painter) Hex(hex string) Style {
	st := p.Plain()
	if !st.on || hex == "" {
		return st
	}
	st.seq = p.profile.Color(hex).Sequence(false)
	return st
}

// On adds a background color to a style.
func (p *Painter) On(st Style, bg Slot) Style { return p.OnHex(st, p.Pal[bg]) }

func (p *Painter) OnHex(st Style, hex string) Style {
	if !p.Enabled() || hex == "" {
		return st
	}
	st.on = true
	return st.add(p.profile.Color(hex).Sequence(true))
}

// Gradient colors each rune of s along a blend between two slots.
func (p *Painter) Gradient(s string, from, to Slot, bold bool) string {
	if !p.Enabled() {
		return s
	}
	a, err1 := colorful.Hex(p.Pal[from])
	b, err2 := colorful.Hex(p.Pal[to])
	runes := []rune(s)
	if err1 != nil || err2 != nil || len(runes) < 2 || p.profile != termenv.TrueColor {
		st := p.Fg(from)
		if bold {
			st = st.Bold()
		}
		return st.Render(s)
	}
	var sb strings.Builder
	for i, r := range runes {
		st := p.Hex(a.BlendLuv(b, float64(i)/float64(len(runes)-1)).Clamped().Hex())
		if bold {
			st = st.Bold()
		}
		sb.WriteString(st.Seq())
		sb.WriteRune(r)
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}

// Blend mixes two slots (t=0 → a, t=1 → b) and returns a hex color.
func (p *Painter) Blend(a, b Slot, t float64) string {
	ca, err1 := colorful.Hex(p.Pal[a])
	cb, err2 := colorful.Hex(p.Pal[b])
	if err1 != nil || err2 != nil {
		return p.Pal[a]
	}
	return ca.BlendLuv(cb, t).Clamped().Hex()
}

// BlendHex mixes two hex colors (t=0 → a, t=1 → b).
func BlendHex(a, b string, t float64) string {
	ca, err1 := colorful.Hex(a)
	cb, err2 := colorful.Hex(b)
	if err1 != nil || err2 != nil {
		return a
	}
	return ca.BlendLuv(cb, t).Clamped().Hex()
}

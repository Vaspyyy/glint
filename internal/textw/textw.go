// Package textw measures the display width of styled terminal text.
//
// It exists instead of charmbracelet/x/ansi because that pulls in
// go-runewidth, whose init() builds lookup tables for ~20ms on every launch.
package textw

import (
	"strings"

	"github.com/rivo/uniseg"
)

// Width returns the number of terminal cells s occupies, ignoring CSI and OSC
// escape sequences (colors, hyperlinks).
func Width(s string) int {
	if strings.IndexByte(s, 0x1b) >= 0 {
		s = Strip(s)
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return uniseg.StringWidth(s)
		}
	}
	return len(s)
}

func Strip(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = escEnd(s, i)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// Truncate cuts s to at most w cells, keeping escape sequences intact and
// appending "…" when something was cut. A reset is appended if styled.
func Truncate(s string, w int) string {
	if Width(s) <= w {
		return s
	}
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	styled := false
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := escEnd(s, i)
			b.WriteString(s[i:j])
			styled = true
			i = j
			continue
		}
		cluster, rest, cw, _ := uniseg.FirstGraphemeClusterInString(s[i:], -1)
		if used+cw > w-1 {
			break
		}
		b.WriteString(cluster)
		used += cw
		i = len(s) - len(rest)
	}
	b.WriteString("…")
	if styled {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// Pad right-pads s with spaces to w cells (truncating if longer).
func Pad(s string, w int) string {
	sw := Width(s)
	if sw > w {
		s = Truncate(s, w)
		sw = Width(s)
	}
	return s + strings.Repeat(" ", max(0, w-sw))
}

func escEnd(s string, i int) int {
	if i+1 >= len(s) {
		return i + 1
	}
	switch s[i+1] {
	case '[':
		i += 2
		for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
			i++
		}
		return min(i+1, len(s))
	case ']', '_', 'P':
		i += 2
		for i < len(s) {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			i++
		}
		return i
	}
	return i + 2
}

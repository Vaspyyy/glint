package agent

import (
	"os"

	"github.com/Vaspyyy/prism/outline"
)

// Symbol is a definition found by the outline rules.
type Symbol = outline.Symbol

// HasOutline reports whether the file type has outline rules.
func HasOutline(name string) bool { return outline.Supported(name) }

// Outline returns the symbols of a source file (at most limit).
func Outline(path string, limit int) []Symbol {
	st, err := os.Stat(path)
	if err != nil || st.Size() > outline.MaxSize {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return outline.Parse(path, data, limit)
}

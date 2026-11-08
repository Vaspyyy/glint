// Package outline moved to github.com/Vaspyyy/prism/outline, which glint,
// seek and recap now share. This package only forwards to it.
//
// Deprecated: import github.com/Vaspyyy/prism/outline instead. This
// package will be removed in a later release.
package outline

import po "github.com/Vaspyyy/prism/outline"

// Symbol is one definition.
//
// Deprecated: use github.com/Vaspyyy/prism/outline.Symbol.
type Symbol = po.Symbol

// MaxSize is the largest file Parse looks at.
//
// Deprecated: use github.com/Vaspyyy/prism/outline.MaxSize.
const MaxSize = po.MaxSize

// Supported reports whether the file type has outline rules.
//
// Deprecated: use github.com/Vaspyyy/prism/outline.Supported.
func Supported(name string) bool { return po.Supported(name) }

// Parse returns the symbols of a file's contents (at most limit), with
// their ends and parents. name only selects the language.
//
// Deprecated: use github.com/Vaspyyy/prism/outline.Parse.
func Parse(name string, data []byte, limit int) []Symbol { return po.Parse(name, data, limit) }

// Qualified is the symbol's name with its parents' names: "Repo.Open".
//
// Deprecated: use github.com/Vaspyyy/prism/outline.Qualified.
func Qualified(syms []Symbol, i int) string { return po.Qualified(syms, i) }

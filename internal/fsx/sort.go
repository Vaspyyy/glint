package fsx

import (
	"path/filepath"
	"slices"
	"strings"
)

var SortKeys = []string{"name", "size", "time", "ext", "none"}

// Sort orders entries. groupDirs is "first", "last" or "none"; "." and ".."
// always stay on top and reversing never moves directories across files.
func Sort(es []*Entry, key string, reverse bool, groupDirs string) {
	if key == "none" {
		return
	}
	slices.SortStableFunc(es, func(a, b *Entry) int {
		if ra, rb := dotRank(a.Name), dotRank(b.Name); ra != rb {
			return ra - rb
		}
		if groupDirs != "none" && a.IsDir != b.IsDir {
			if (groupDirs == "first") == a.IsDir {
				return -1
			}
			return 1
		}
		c := compare(a, b, key)
		if reverse {
			c = -c
		}
		return c
	})
}

func dotRank(name string) int {
	switch name {
	case ".":
		return 0
	case "..":
		return 1
	}
	return 2
}

func size(e *Entry) int64 {
	if e.HasDirSize {
		return e.DirSize
	}
	return e.Size
}

func compare(a, b *Entry, key string) int {
	switch key {
	case "size":
		if sa, sb := size(a), size(b); sa != sb {
			if sa > sb {
				return -1
			}
			return 1
		}
	case "time":
		if c := b.ModTime.Compare(a.ModTime); c != 0 {
			return c
		}
	case "ext":
		ea := strings.ToLower(filepath.Ext(a.Name))
		eb := strings.ToLower(filepath.Ext(b.Name))
		if c := strings.Compare(ea, eb); c != 0 {
			return c
		}
	}
	return NaturalCompare(a.Name, b.Name)
}

// NaturalCompare sorts case-insensitively, ignores leading punctuation
// (. _ -) and compares digit runs numerically, so file2 < file10, .config
// sits next to config and _drafts next to drafts.
func NaturalCompare(a, b string) int {
	la := Fold(strings.TrimLeft(a, "._-"))
	lb := Fold(strings.TrimLeft(b, "._-"))
	if c := natural(la, lb); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func natural(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch ra, rb := len(a)-i, len(b)-j; {
	case ra < rb:
		return -1
	case ra > rb:
		return 1
	}
	return 0
}

var foldTable = func() map[rune]string {
	m := map[rune]string{}
	for from, to := range map[string]string{
		"àáâãäåāăą": "a", "æ": "ae", "çćčĉċ": "c", "ďđ": "d", "èéêëēĕėęě": "e",
		"ĝğġģ": "g", "ĥħ": "h", "ìíîïĩīĭįı": "i", "ĵ": "j", "ķ": "k", "ĺļľŀł": "l",
		"ñńņňŉ": "n", "òóôõöøōŏő": "o", "œ": "oe", "ŕŗř": "r", "śŝşšș": "s", "ß": "ss",
		"ţťŧț": "t", "ùúûüũūŭůűų": "u", "ŵ": "w", "ýÿŷ": "y", "źżž": "z", "þ": "th",
	} {
		for _, r := range from {
			m[r] = to
		}
	}
	return m
}()

// Fold lowercases and strips diacritics, so "Öffentlich" sorts with the O's.
func Fold(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if f, ok := foldTable[r]; ok {
			b.WriteString(f)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

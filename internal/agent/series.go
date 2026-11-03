package agent

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Vaspyyy/glint/internal/git"
)

// Runs of near-identical entries (frame_001.png … frame_240.png, base-1.txt …
// base-3.txt, run1/ … run7/) cost many lines and tell an agent nothing the
// first one doesn't. groupSeries folds them into a single node.

var digitRun = regexp.MustCompile(`\d+`)

const minSeries = 3

func groupSeries(n *node) {
	if !n.dir || len(n.kids) < minSeries {
		for _, k := range n.kids {
			if k.dir {
				groupSeries(k)
			}
		}
		return
	}
	for _, k := range n.kids {
		if k.dir {
			groupSeries(k)
		}
	}

	// files: same name once digit runs are wildcarded
	fileGroups := map[string][]*node{}
	// dirs: same name once digits are dropped (ab, ab2, ab3 … are one family)
	dirGroups := map[string][]*node{}
	for _, k := range n.kids {
		if k.series != nil || k.skipped || k.empty || k.sub {
			continue
		}
		if !digitRun.MatchString(k.name) && !k.dir {
			continue
		}
		if k.dir {
			dirGroups[digitRun.ReplaceAllString(k.name, "")] = append(dirGroups[digitRun.ReplaceAllString(k.name, "")], k)
		} else {
			key := digitRun.ReplaceAllString(k.name, "#")
			fileGroups[key] = append(fileGroups[key], k)
		}
	}

	replace := map[*node]*node{} // first member → group node
	drop := map[*node]bool{}
	for key, g := range fileGroups {
		if len(g) < minSeries {
			continue
		}
		s := &node{name: seriesName(key, g), rel: g[0].rel, abs: g[0].abs, series: g, git: strongest(g)}
		for _, m := range g {
			s.size += m.size
			if m.mtime.After(s.mtime) {
				s.mtime = m.mtime
			}
			drop[m] = true
		}
		replace[g[0]] = s
	}
	for _, g := range dirGroups {
		withDigits := 0
		for _, m := range g {
			if digitRun.MatchString(m.name) {
				withDigits++
			}
		}
		if len(g) < minSeries || withDigits < minSeries-1 {
			continue
		}
		// keep the first one as a normal, expandable folder; fold the rest
		rest := g[1:]
		s := newDir(rest[0].name, rest[0].rel, rest[0].abs)
		s.series, s.like, s.git = rest, g[0].name, strongest(rest)
		for _, m := range rest {
			s.files += m.files
			s.size += m.size
			for e, c := range m.exts {
				s.exts[e] += c
			}
			if m.mtime.After(s.mtime) {
				s.mtime = m.mtime
			}
			drop[m] = true
		}
		replace[rest[0]] = s
	}
	if len(drop) == 0 {
		return
	}
	kids := make([]*node, 0, len(n.kids))
	for _, k := range n.kids {
		if s, ok := replace[k]; ok {
			kids = append(kids, s)
		} else if !drop[k] {
			kids = append(kids, k)
		}
	}
	n.kids = kids
}

// seriesName renders base-#.txt for members base-1..3 as "base-{1..3}.txt",
// or with a wildcard when the numbers aren't a simple contiguous range.
func seriesName(key string, g []*node) string {
	if strings.Count(key, "#") != 1 {
		return strings.ReplaceAll(key, "#", "*")
	}
	pre, suf, _ := strings.Cut(key, "#")
	lo, hi := -1, -1
	var loS, hiS string
	seen := map[int]bool{}
	for _, m := range g {
		num := strings.TrimSuffix(strings.TrimPrefix(m.name, pre), suf)
		v, err := strconv.Atoi(num)
		if err != nil || seen[v] {
			return pre + "*" + suf
		}
		seen[v] = true
		if lo < 0 || v < lo {
			lo, loS = v, num
		}
		if v > hi {
			hi, hiS = v, num
		}
	}
	// keep the real spelling (zero padding!) so the endpoints are real names
	if hi-lo+1 == len(g) {
		return pre + "{" + loS + ".." + hiS + "}" + suf
	}
	return pre + "*" + suf
}

func strongest(g []*node) (st git.Status) {
	for _, m := range g {
		if !m.git.Clean() && !m.git.Ignored() {
			return m.git
		}
	}
	return
}

// seriesLines summarises the line counts / sizes of a file series.
func seriesLines(s *node) string {
	lo, hi, bins := -1, 0, 0
	for _, m := range s.series {
		if m.binary {
			bins++
			continue
		}
		if lo < 0 || m.lines < lo {
			lo = m.lines
		}
		hi = max(hi, m.lines)
	}
	switch {
	case bins == len(s.series):
		note := s.series[0].note
		for _, m := range s.series {
			if m.note != note {
				note = ""
			}
		}
		if note != "" {
			return note + " each, " + human(s.size) + " total"
		}
		return "bin " + human(s.size) + " total"
	case lo == hi:
		return count(lo) + "L each"
	case lo < 0:
		return human(s.size) + " total"
	}
	return count(lo) + "–" + count(hi) + "L"
}

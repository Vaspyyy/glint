// Package outline finds the definitions in a source file (functions, types,
// classes, Markdown headings) with cheap per-language rules, and where each
// one ends. It is not a parser: it is enough to tell where things live and
// which definition a changed line belongs to, and fast enough to run on
// every file of a repository.
package outline

import (
	"bufio"
	"bytes"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"strings"
)

// Symbol is one definition.
type Symbol struct {
	Line   int    // first line, 1-based
	End    int    // last line (inclusive); Line if the end isn't known
	Kind   string // "func", "type", "class", "h2", …
	Name   string
	Sig    string // the defining line, trimmed: name plus signature
	Parent int    // index of the enclosing symbol, or -1
}

type rule struct {
	re      *regexp.Regexp
	kind    string // "" means: take the kind from capture group 1
	qualify bool   // group 1 (if matched) qualifies the name: a Go method's receiver type
	need    []string
}

func r(kind, pattern string) rule { return rule{re: regexp.MustCompile(pattern), kind: kind} }

// prepare works out the literals a rule's matches contain (one of them),
// so that lines without any skip the regex.
func prepare(rules []rule) []rule {
	for i := range rules {
		if sre, err := syntax.Parse(rules[i].re.String(), syntax.Perl); err == nil {
			rules[i].need = required(sre.Simplify())
		}
	}
	return rules
}

// required returns literals one of which every match of re contains, or
// nil if there is no such set.
func required(re *syntax.Regexp) []string {
	switch re.Op {
	case syntax.OpLiteral:
		if re.Flags&syntax.FoldCase != 0 {
			return nil
		}
		return []string{string(re.Rune)}
	case syntax.OpCapture, syntax.OpPlus:
		return required(re.Sub[0])
	case syntax.OpRepeat:
		if re.Min >= 1 {
			return required(re.Sub[0])
		}
	case syntax.OpConcat:
		var best []string
		for _, s := range re.Sub {
			if r := required(s); r != nil && (best == nil || shortest(r) > shortest(best)) {
				best = r
			}
		}
		return best
	case syntax.OpAlternate:
		var all []string
		for _, s := range re.Sub {
			r := required(s)
			if r == nil {
				return nil
			}
			all = append(all, r...)
		}
		return all
	}
	return nil
}

func shortest(lits []string) int {
	n := -1
	for _, l := range lits {
		if n < 0 || len(l) < n {
			n = len(l)
		}
	}
	return n
}

func (ru *rule) mayMatch(line string) bool {
	if ru.need == nil {
		return true
	}
	for _, l := range ru.need {
		if strings.Contains(line, l) {
			return true
		}
	}
	return false
}

// how a language marks where a definition ends
type style int

const (
	braces   style = iota // { … }
	indent                // the next line indented as much or less (Python, GDScript)
	keyword               // indentation, plus a closing "end" line (Ruby, Lua)
	headings              // the next heading of the same or a higher level
)

type lang struct {
	rules []rule
	style style
	hash  bool // # starts a comment
	js    bool // '…' are strings and /…/ can be a regex literal
	multi bool // "…" may continue on the next line (Rust)
}

var (
	goRules = prepare([]rule{
		{re: regexp.MustCompile(`^func (?:\(\s*(?:\w+\s+)?\*?(\w+)(?:\[[^\]]*\])?\s*\) )?(\w+)`), kind: "func", qualify: true},
		r("", `^(type) (\w+)`),
	})
	rustRules = prepare([]rule{
		r("", `^\s{0,4}(?:pub(?:\([^)]*\))? )?(?:async )?(?:unsafe )?(?:const )?(fn|struct|enum|trait|mod|type|union|static|macro_rules!) (\w+)`),
		r("impl", `^impl(?:<[^>]*>)? ([\w:<>, ]+?)(?: \{|$| where)`),
	})
	pyRules = prepare([]rule{
		r("", `^(?:    )?(?:async )?(def|class) (\w+)`),
	})
	jsRules = prepare([]rule{
		// also nested declarations (helpers inside a factory function)
		r("function", `^\s*(?:export )?(?:default )?(?:async )?function\*? ?(\w+)`),
		r("", `^(?:export )?(?:default )?(?:abstract )?(class|interface|enum|type) (\w+)`),
		r("const", `^\s*(?:export )?(?:const|let|var) (\w+)\s*(?::[^=]+)?=\s*(?:async\s*)?(?:\([^)]*\)|\w+)\s*(?::[^=]+)?=>`),
		r("method", `^(?:  |    |\t)(?:public |private |protected |static |async |readonly |override |get |set )*\*?(\w+)\s*\([^)]*\)\s*(?::[^{]+)?\{\s*$`),
	})
	cRules = prepare([]rule{
		r("", `^(?:typedef )?(struct|enum|union|class|namespace) (\w+)`),
		r("define", `^#define (\w+)`),
		// definitions start in column 0: a return type, the name, a parameter
		// list, then either the end of the line or the opening brace
		r("func", `^(?:[A-Za-z_][\w:<>,]*[\s*&]+)+([A-Za-z_][\w:~]*)\s*\([^;{}]*\)\s*(?:const\s*)?(?:\{.*)?$`),
		r("func", `^(?:[A-Za-z_][\w:<>,]*[\s*&]+)+([A-Za-z_][\w:~]*)\s*\([^;{})]*,\s*$`),
	})
	javaRules = prepare([]rule{
		r("", `^\s{0,4}(?:public |private |protected |internal |abstract |final |sealed |data |open |static )*(class|interface|enum|record|object) (\w+)`),
		r("fun", `^\s{0,4}(?:public |private |protected |internal |override |suspend |inline |open )*fun (?:<[^>]*> )?(?:\w+\.)?(\w+)`),
		r("method", `^    (?:public |private |protected |static |final |synchronized |abstract )+[\w<>\[\], ]+ (\w+)\s*\(`),
	})
	gdRules = prepare([]rule{
		r("", `^(func|class_name|signal|class|enum) (\w+)`),
	})
	shRules = prepare([]rule{
		r("function", `^(?:function )?([\w-]+)\s*\(\)\s*\{?`),
		r("function", `^function ([\w-]+)`),
	})
	luaRules = prepare([]rule{r("function", `^(?:local )?function ([\w.:]+)`)})
	rbRules  = prepare([]rule{r("", `^\s{0,2}(def|class|module) ([\w.?!:]+)`)})
	mdRules  = prepare([]rule{r("", `^(#{1,3}) (.+)`)})
)

var (
	goLang   = lang{rules: goRules}
	rustLang = lang{rules: rustRules, multi: true}
	pyLang   = lang{rules: pyRules, style: indent, hash: true}
	jsLang   = lang{rules: jsRules, js: true}
	cLang    = lang{rules: cRules}
	javaLang = lang{rules: javaRules}
	gdLang   = lang{rules: gdRules, style: indent, hash: true}
	shLang   = lang{rules: shRules, hash: true}
	luaLang  = lang{rules: luaRules, style: keyword}
	rbLang   = lang{rules: rbRules, style: keyword, hash: true}
	mdLang   = lang{rules: mdRules, style: headings}
)

var langByExt = map[string]*lang{
	"go": &goLang, "rs": &rustLang, "py": &pyLang, "pyi": &pyLang,
	"js": &jsLang, "mjs": &jsLang, "cjs": &jsLang, "ts": &jsLang, "tsx": &jsLang, "jsx": &jsLang, "mts": &jsLang,
	"svelte": &jsLang, "vue": &jsLang,
	"c": &cLang, "h": &cLang, "cc": &cLang, "cpp": &cLang, "cxx": &cLang, "hpp": &cLang, "hh": &cLang,
	"java": &javaLang, "kt": &javaLang, "kts": &javaLang, "cs": &javaLang, "scala": &javaLang,
	"gd": &gdLang, "sh": &shLang, "bash": &shLang, "zsh": &shLang,
	"lua": &luaLang, "rb": &rbLang, "md": &mdLang, "markdown": &mdLang,
}

func langOf(name string) *lang {
	return langByExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))]
}

// Supported reports whether the file type has outline rules.
func Supported(name string) bool { return langOf(name) != nil }

// MaxSize is the largest file Parse looks at.
const MaxSize = 4 << 20

// Parse returns the symbols of a file's contents (at most limit), with
// their ends and parents. name only selects the language.
func Parse(name string, data []byte, limit int) []Symbol {
	lg := langOf(name)
	if lg == nil || len(data) > MaxSize || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return nil
	}
	lines := splitLines(data)
	md := lg.style == headings
	var out []Symbol
	inFence := false
	sc := scanner{}
	for i, line := range lines {
		if md && strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if inFence {
			continue
		}
		if !md && !lg.hash {
			line = sc.stripComments(line)
			if strings.TrimSpace(line) == "" {
				continue
			}
		}
		for _, ru := range lg.rules {
			if !ru.mayMatch(line) {
				continue
			}
			m := ru.re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			kind, name := ru.kind, m[len(m)-1]
			if kind == "" && len(m) >= 3 {
				kind = m[1]
			}
			if ru.qualify && m[1] != "" {
				name = m[1] + "." + name
			}
			if md {
				kind = "h" + string(rune('0'+len(m[1])))
			}
			if isNoise(name) {
				break
			}
			sig := strings.TrimSpace(line)
			if i := strings.IndexByte(sig, '{'); i > 0 && !md {
				sig = strings.TrimSpace(sig[:i]) // signature only, not a one-line body
			}
			if r := []rune(sig); len(r) > 110 {
				sig = string(r[:109]) + "…"
			}
			out = append(out, Symbol{Line: i + 1, End: i + 1, Kind: kind, Name: strings.TrimSpace(name), Sig: sig, Parent: -1})
			break
		}
		if len(out) >= limit {
			break
		}
	}
	findEnds(lg, lines, out)
	findParents(out)
	return out
}

func splitLines(data []byte) []string {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), MaxSize+1)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func findEnds(lg *lang, lines []string, syms []Symbol) {
	for k := range syms {
		s := &syms[k]
		switch lg.style {
		case braces:
			// stop looking for the opening brace at the next definition
			limit := len(lines)
			if k+1 < len(syms) {
				limit = syms[k+1].Line - 1
			}
			s.End = braceEnd(lines, s.Line-1, limit, lg, s.Kind == "define")
		case indent, keyword:
			s.End = indentEnd(lines, s.Line-1, lg.style == keyword)
		case headings:
			s.End = len(lines)
			for j := k + 1; j < len(syms); j++ {
				if syms[j].Kind <= s.Kind { // "h1" < "h2": same or higher level
					s.End = syms[j].Line - 1
					break
				}
			}
		}
		if s.End < s.Line {
			s.End = s.Line
		}
	}
}

// braceEnd returns the 1-based line of the brace that closes the block
// opened on or after line start (0-based). A declaration without a block
// (`type ID int`, `fn f();`) ends on its own line.
func braceEnd(lines []string, start, limit int, lg *lang, define bool) int {
	if define { // #define with continuation lines
		j := start
		for j+1 < len(lines) && strings.HasSuffix(strings.TrimRight(lines[j], " \t"), "\\") {
			j++
		}
		return j + 1
	}
	sc := scanner{hash: lg.hash, js: lg.js, multi: lg.multi}
	depth, opened, parens := 0, false, 0
	for j := start; j < len(lines); j++ {
		if !opened && j >= limit && j > start {
			return start + 1
		}
		for _, c := range sc.code(lines[j]) {
			if !opened && parens > 0 && c != '(' && c != ')' {
				continue // braces in the parameters ({ a, b } = opts, struct{ x int }) aren't the body
			}
			switch c {
			case '(':
				if !opened {
					parens++
				}
			case ')':
				if !opened {
					parens--
				}
			case '{':
				depth++
				opened = true
			case '}':
				depth--
				if opened && depth == 0 {
					return j + 1
				}
			case ';':
				if !opened && depth == 0 {
					return j + 1
				}
			}
		}
		if !opened && j-start > 30 {
			return start + 1
		}
	}
	if opened {
		return len(lines)
	}
	return start + 1
}

// indentEnd: the block ends before the next non-blank line that is
// indented as much as the definition or less (decorators and comments at
// that level belong to what follows). With endKeyword, a closing "end"
// line at the definition's level is part of it.
func indentEnd(lines []string, start int, endKeyword bool) int {
	base := indentOf(lines[start])
	last := start
	open := strings.Count(lines[start], "(") - strings.Count(lines[start], ")")
	for j := start + 1; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			continue
		}
		if open > 0 { // a signature spread over several lines
			open += strings.Count(lines[j], "(") - strings.Count(lines[j], ")")
			last = j
			continue
		}
		if indentOf(lines[j]) <= base {
			if endKeyword && (t == "end" || strings.HasPrefix(t, "end ") || strings.HasPrefix(t, "end)") || strings.HasPrefix(t, "end,")) {
				return j + 1
			}
			break
		}
		last = j
	}
	return last + 1
}

func indentOf(l string) int {
	n := 0
	for _, c := range l {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// findParents sets each symbol's Parent to the innermost symbol whose
// lines contain it.
func findParents(syms []Symbol) {
	for i := range syms {
		for j := i - 1; j >= 0; j-- {
			if syms[j].Line < syms[i].Line && syms[j].End >= syms[i].End && syms[j].End > syms[j].Line {
				syms[i].Parent = j
				break
			}
		}
	}
}

// Qualified is the symbol's name with its parents' names: "Repo.Open".
func Qualified(syms []Symbol, i int) string {
	name := syms[i].Name
	for p := syms[i].Parent; p >= 0; p = syms[p].Parent {
		if syms[p].Kind == "h1" || syms[p].Kind == "h2" || syms[p].Kind == "h3" {
			break // headings nest, but "Install.Linux" isn't a name anyone uses
		}
		parent := syms[p].Name
		if syms[p].Kind == "impl" { // "impl Shape for Circle": the methods are Circle's
			if _, typ, ok := strings.Cut(parent, " for "); ok {
				parent = typ
			}
		}
		name = parent + "." + name
	}
	return name
}

// scanner returns the code of a line with strings, character literals and
// comments blanked out, carrying multi-line strings and block comments over
// to the next line. Good enough to count braces.
type scanner struct {
	hash    bool
	js      bool // JavaScript: '…' strings and /…/ regex literals
	multi   bool // "…" may span lines
	inBlock bool // /* … */
	inRaw   byte // ` or " for a string that continues on the next line
}

func (s *scanner) code(line string) []byte {
	out := make([]byte, 0, len(line))
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case s.inBlock:
			if strings.HasPrefix(line[i:], "*/") {
				s.inBlock = false
				i++
			}
		case s.inRaw != 0:
			if c == '\\' && s.inRaw != '`' {
				i++
			} else if c == s.inRaw {
				s.inRaw = 0
			}
		case strings.HasPrefix(line[i:], "/*") && !s.hash:
			s.inBlock = true
			i++
		case strings.HasPrefix(line[i:], "//") && !s.hash, c == '#' && s.hash && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return out
		case c == '`':
			s.inRaw = '`'
		case c == '"' || c == '\'' && s.js:
			j := i + 1
			for j < len(line) && line[j] != c {
				if line[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(line) && (s.multi || strings.HasSuffix(line, "\\")) {
				s.inRaw = c // continues on the next line: Rust, or after a backslash
			}
			i = j
		case c == '/' && s.js && regexStart(out):
			// a regex literal: up to the closing slash outside a [class]
			j, class := i+1, false
			for ; j < len(line); j++ {
				if line[j] == '\\' {
					j++
				} else if line[j] == '[' {
					class = true
				} else if line[j] == ']' {
					class = false
				} else if line[j] == '/' && !class {
					break
				}
			}
			if j >= len(line) {
				out = append(out, c) // no closing slash: a division after all
			} else {
				i = j
			}
		case c == '\'':
			// a character literal ('x', '\n', '{'), not a Rust lifetime ('a)
			if j := strings.IndexByte(line[i+1:min(len(line), i+5)], '\''); j >= 0 {
				i += j + 1
			}
		default:
			out = append(out, c)
		}
	}
	return out
}

// regexStart: a slash after this code starts a regex literal, not a
// division (it follows an operator, an opening bracket, or nothing).
func regexStart(code []byte) bool {
	t := strings.TrimRight(string(code), " \t")
	if t == "" {
		return true
	}
	if strings.ContainsRune("(,=:[!&|?{};+-*%<>~^", rune(t[len(t)-1])) {
		return true
	}
	for _, kw := range []string{"return", "typeof", "case", "in", "of", "delete", "void", "yield", "await"} {
		if strings.HasSuffix(t, kw) && (len(t) == len(kw) || !isIdent(t[len(t)-len(kw)-1])) {
			return true
		}
	}
	return false
}

func isIdent(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// stripComments returns the line without its /* */ and // comments,
// keeping string literals as they are ("/*" in a string isn't a comment).
func (s *scanner) stripComments(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case s.inBlock:
			if strings.HasPrefix(line[i:], "*/") {
				s.inBlock = false
				i++
			}
			continue
		case s.inRaw != 0:
			if c == '\\' && s.inRaw != '`' && i+1 < len(line) {
				b.WriteByte(c)
				i++
				c = line[i]
			} else if c == s.inRaw {
				s.inRaw = 0
			}
		case strings.HasPrefix(line[i:], "/*"):
			s.inBlock = true
			i++
			continue
		case strings.HasPrefix(line[i:], "//"):
			return b.String()
		case c == '`':
			s.inRaw = '`'
		case c == '"':
			j := i + 1
			for j < len(line) && line[j] != '"' {
				if line[j] == '\\' {
					j++
				}
				j++
			}
			b.WriteString(line[i:min(j+1, len(line))])
			i = j
			continue
		}
		b.WriteByte(c)
	}
	if s.inRaw == '"' {
		s.inRaw = 0 // an unterminated "…" doesn't carry over in most languages
	}
	return b.String()
}

func isNoise(name string) bool {
	switch name {
	case "if", "for", "while", "switch", "return", "else", "sizeof", "catch", "main_", "do", "case", "":
		return true
	}
	return false
}

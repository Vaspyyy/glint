package agent

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Symbol is a top-level-ish definition found by a cheap per-language regex.
// It's not a parser, just enough to tell an agent where things live so it
// can read a file with an offset instead of reading all of it.
type Symbol struct {
	Line int
	Kind string
	Name string
	Sig  string // the defining line, trimmed: name plus signature
}

type rule struct {
	re   *regexp.Regexp
	kind string // "" means: take the kind from capture group 1
}

func r(kind, pattern string) rule { return rule{regexp.MustCompile(pattern), kind} }

var (
	goRules = []rule{
		r("func", `^func (?:\([^)]*\) )?(\w+)`),
		r("", `^(type) (\w+)`),
	}
	rustRules = []rule{
		r("", `^\s{0,4}(?:pub(?:\([^)]*\))? )?(?:async )?(?:unsafe )?(?:const )?(fn|struct|enum|trait|mod|type|union|static|macro_rules!) (\w+)`),
		r("impl", `^impl(?:<[^>]*>)? ([\w:<>, ]+?)(?: \{|$| where)`),
	}
	pyRules = []rule{
		r("", `^(?:    )?(?:async )?(def|class) (\w+)`),
	}
	jsRules = []rule{
		r("function", `^(?:export )?(?:default )?(?:async )?function\*? ?(\w+)`),
		r("", `^(?:export )?(?:default )?(?:abstract )?(class|interface|enum|type) (\w+)`),
		r("const", `^(?:export )?(?:const|let|var) (\w+)\s*(?::[^=]+)?=\s*(?:async\s*)?(?:\([^)]*\)|\w+)\s*(?::[^=]+)?=>`),
		r("method", `^  (?:public |private |protected |static |async |readonly )*(\w+)\s*\([^)]*\)\s*(?::[^{]+)?\{\s*$`),
	}
	cRules = []rule{
		r("", `^(?:typedef )?(struct|enum|union|class|namespace) (\w+)`),
		r("define", `^#define (\w+)`),
		// definitions start in column 0: a return type, the name, a parameter
		// list, then either the end of the line or the opening brace
		r("func", `^(?:[A-Za-z_][\w:<>,]*[\s*&]+)+([A-Za-z_][\w:~]*)\s*\([^;{}]*\)\s*(?:const\s*)?(?:\{.*)?$`),
		r("func", `^(?:[A-Za-z_][\w:<>,]*[\s*&]+)+([A-Za-z_][\w:~]*)\s*\([^;{})]*,\s*$`),
	}
	javaRules = []rule{
		r("", `^\s{0,4}(?:public |private |protected |internal |abstract |final |sealed |data |open |static )*(class|interface|enum|record|object) (\w+)`),
		r("fun", `^\s{0,4}(?:public |private |protected |internal |override |suspend |inline |open )*fun (?:<[^>]*> )?(?:\w+\.)?(\w+)`),
		r("method", `^    (?:public |private |protected |static |final |synchronized |abstract )+[\w<>\[\], ]+ (\w+)\s*\(`),
	}
	gdRules = []rule{
		r("", `^(func|class_name|signal|class|enum) (\w+)`),
	}
	shRules = []rule{
		r("function", `^(?:function )?([\w-]+)\s*\(\)\s*\{?`),
		r("function", `^function ([\w-]+)`),
	}
	luaRules = []rule{r("function", `^(?:local )?function ([\w.:]+)`)}
	rbRules  = []rule{r("", `^\s{0,2}(def|class|module) ([\w.?!:]+)`)}
	mdRules  = []rule{r("", `^(#{1,3}) (.+)`)}
)

var rulesByExt = map[string][]rule{
	"go": goRules, "rs": rustRules, "py": pyRules, "pyi": pyRules,
	"js": jsRules, "mjs": jsRules, "cjs": jsRules, "ts": jsRules, "tsx": jsRules, "jsx": jsRules, "mts": jsRules,
	"svelte": jsRules, "vue": jsRules,
	"c": cRules, "h": cRules, "cc": cRules, "cpp": cRules, "cxx": cRules, "hpp": cRules, "hh": cRules,
	"java": javaRules, "kt": javaRules, "kts": javaRules, "cs": javaRules, "scala": javaRules,
	"gd": gdRules, "sh": shRules, "bash": shRules, "zsh": shRules,
	"lua": luaRules, "rb": rbRules, "md": mdRules, "markdown": mdRules,
}

// HasOutline reports whether the file type has outline rules.
func HasOutline(name string) bool {
	_, ok := rulesByExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))]
	return ok
}

// Outline returns the symbols of a source file (at most limit).
func Outline(path string, limit int) []Symbol {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	rules := rulesByExt[ext]
	if rules == nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 4<<20 || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return nil
	}
	md := ext == "md" || ext == "markdown"
	blockComments := !md && !slicesHas(hashComment, ext)
	var out []Symbol
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	n := 0
	inFence, inComment := false, false
	for sc.Scan() {
		n++
		line := sc.Text()
		if md && strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if inFence {
			continue
		}
		if blockComments {
			line, inComment = stripComments(line, inComment)
			if strings.TrimSpace(line) == "" {
				continue
			}
		}
		for _, ru := range rules {
			m := ru.re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			kind, name := ru.kind, m[len(m)-1]
			if kind == "" && len(m) >= 3 {
				kind = m[1]
			}
			if md {
				kind = "h" + itoa(len(m[1]))
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
			out = append(out, Symbol{n, kind, strings.TrimSpace(name), sig})
			break
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

var hashComment = []string{"py", "pyi", "sh", "bash", "zsh", "rb", "gd", "lua"}

func slicesHas(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// stripComments removes /* */ and // comments from a line, tracking block
// comments across lines. String literals are not parsed; good enough for an
// outline, and it stops prose in doc comments from looking like code.
func stripComments(line string, inBlock bool) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(line); {
		if inBlock {
			end := strings.Index(line[i:], "*/")
			if end < 0 {
				return b.String(), true
			}
			i += end + 2
			inBlock = false
			continue
		}
		if strings.HasPrefix(line[i:], "/*") {
			inBlock = true
			i += 2
			continue
		}
		if strings.HasPrefix(line[i:], "//") {
			break
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String(), inBlock
}

func isNoise(name string) bool {
	switch name {
	case "if", "for", "while", "switch", "return", "else", "sizeof", "catch", "main_", "do", "case", "":
		return true
	}
	return false
}

func itoa(n int) string {
	return string(rune('0' + n))
}

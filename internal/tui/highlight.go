package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Vaspyyy/glint/internal/theme"
)

// A deliberately tiny highlighter: comments, strings, numbers, keywords,
// calls and Types. Good enough for a preview, and zero startup cost
// (unlike chroma's hundreds of lexers registering at init).

type lang struct {
	line       string // line comment
	blockStart string
	blockEnd   string
}

var (
	cLike = lang{"//", "/*", "*/"}
	hash  = lang{line: "#"}
	dash  = lang{line: "--"}
)

var langByExt = map[string]lang{
	"go": cLike, "rs": cLike, "js": cLike, "mjs": cLike, "cjs": cLike, "ts": cLike, "tsx": cLike, "jsx": cLike,
	"java": cLike, "kt": cLike, "kts": cLike, "c": cLike, "h": cLike, "cpp": cLike, "cc": cLike, "hpp": cLike,
	"cs": cLike, "swift": cLike, "dart": cLike, "zig": cLike, "php": cLike, "css": cLike, "scss": cLike,
	"json": cLike, "jsonc": cLike, "gd": hash, "svelte": cLike, "vue": cLike, "glsl": cLike, "wgsl": cLike,
	"py": hash, "sh": hash, "bash": hash, "zsh": hash, "fish": hash, "rb": hash, "toml": hash, "yaml": hash,
	"yml": hash, "conf": hash, "ini": hash, "nix": hash, "pl": hash, "r": hash, "ex": hash, "exs": hash,
	"makefile": hash, "dockerfile": hash, "pkgbuild": hash, "cmake": hash, "env": hash, "mod": cLike,
	"lua": dash, "sql": dash, "hs": dash,
}

var keywords = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range strings.Fields(`func function fn def class struct enum interface type return if else elif
		for while loop match switch case break continue import from package use mod pub let const var mut static
		new nil null None true false True False self this super async await try catch except finally raise throw
		defer go chan select range in is not and or impl trait where export default extends implements public
		private protected void local then end do elseif lambda with as yield unsafe extern typedef include define
		map dyn ref crate override final abstract val when object fun suspend namespace using template typename
		goto sizeof operator virtual inline readonly declare keyof typeof instanceof delete of`) {
		m[k] = true
	}
	return m
}()

type hl struct {
	kw, str, num, com, fn, typ, punct, text theme.Style
	inBlock                                 bool
	lang                                    lang
}

func newHL(p *theme.Painter, l lang) *hl {
	return &hl{
		kw: p.Fg(theme.Purple), str: p.Fg(theme.Green), num: p.Fg(theme.Orange),
		com: p.Fg(theme.Faint).Italic(), fn: p.Fg(theme.Blue), typ: p.Fg(theme.Yellow),
		punct: p.Fg(theme.Subtle), text: p.Fg(theme.Text), lang: l,
	}
}

func isIdent(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func (h *hl) line(s string) string {
	var b strings.Builder
	l := h.lang
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case h.inBlock:
			end := strings.Index(rest, l.blockEnd)
			if end < 0 {
				b.WriteString(h.com.Render(rest))
				return b.String()
			}
			b.WriteString(h.com.Render(rest[:end+len(l.blockEnd)]))
			i += end + len(l.blockEnd)
			h.inBlock = false
		case l.line != "" && strings.HasPrefix(rest, l.line):
			b.WriteString(h.com.Render(rest))
			return b.String()
		case l.blockStart != "" && strings.HasPrefix(rest, l.blockStart):
			h.inBlock = true
			b.WriteString(h.com.Render(l.blockStart))
			i += len(l.blockStart)
		case rest[0] == '"' || rest[0] == '\'' || rest[0] == '`':
			q := rest[0]
			j := 1
			for j < len(rest) && rest[j] != q {
				if rest[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(rest))
			b.WriteString(h.str.Render(rest[:j]))
			i += j
		default:
			r, size := utf8.DecodeRuneInString(rest)
			switch {
			case unicode.IsDigit(r):
				j := 0
				for j < len(rest) && (isIdent(rune(rest[j])) || rest[j] == '.') {
					j++
				}
				b.WriteString(h.num.Render(rest[:j]))
				i += j
			case isIdent(r):
				j := 0
				for j < len(rest) {
					rr, sz := utf8.DecodeRuneInString(rest[j:])
					if !isIdent(rr) {
						break
					}
					j += sz
				}
				word := rest[:j]
				after := strings.TrimLeft(rest[j:], " ")
				switch {
				case keywords[word]:
					b.WriteString(h.kw.Render(word))
				case strings.HasPrefix(after, "("):
					b.WriteString(h.fn.Render(word))
				case unicode.IsUpper(r):
					b.WriteString(h.typ.Render(word))
				default:
					b.WriteString(h.text.Render(word))
				}
				i += j
			case r == ' ':
				b.WriteByte(' ')
				i++
			default:
				b.WriteString(h.punct.Render(rest[:size]))
				i += size
			}
		}
	}
	return b.String()
}

// markdown styles headings, fences, bullets and quotes.
func markdown(p *theme.Painter, lines []string) []string {
	out := make([]string, len(lines))
	inFence := false
	heads := []theme.Slot{theme.Pink, theme.Purple, theme.Blue, theme.Cyan, theme.Teal, theme.Green}
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "```"):
			inFence = !inFence
			out[i] = p.Fg(theme.Faint).Render(l)
		case inFence:
			out[i] = p.Fg(theme.Green).Render(l)
		case strings.HasPrefix(t, "#"):
			level := len(t) - len(strings.TrimLeft(t, "#"))
			out[i] = p.Fg(heads[min(level, len(heads))-1]).Bold().Render(l)
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ "):
			idx := strings.Index(l, t[:2])
			out[i] = l[:idx] + p.Fg(theme.Pink).Render("•") + " " + inlineCode(p, t[2:])
		case strings.HasPrefix(t, ">"):
			out[i] = p.Fg(theme.Muted).Italic().Render(l)
		default:
			out[i] = inlineCode(p, l)
		}
	}
	return out
}

// inlineCode styles `code` spans and **bold** runs.
func inlineCode(p *theme.Painter, s string) string {
	var b strings.Builder
	for i, part := range strings.Split(s, "`") {
		if i%2 == 1 {
			b.WriteString(p.Fg(theme.Green).Render(part))
			continue
		}
		for j, run := range strings.Split(part, "**") {
			st := p.Fg(theme.Text)
			if j%2 == 1 {
				st = p.Fg(theme.Orange).Bold()
			}
			b.WriteString(st.Render(run))
		}
	}
	return b.String()
}

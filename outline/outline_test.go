package outline

import (
	"fmt"
	"strings"
	"testing"
)

func spans(name, src string) string {
	syms := Parse(name, []byte(strings.TrimPrefix(src, "\n")), 100)
	var b []string
	for i, s := range syms {
		b = append(b, fmt.Sprintf("%s %s %d-%d", s.Kind, Qualified(syms, i), s.Line, s.End))
	}
	return strings.Join(b, "; ")
}

func TestEnds(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"a.go", `
package a

type ID int

type Repo struct {
	path string // a } in a comment
}

func (r *Repo) Open(s string) error {
	if s == "}" {
		return nil
	}
	x := ` + "`" + `raw {
string` + "`" + `
	_ = x
	return nil
}

func helper(
	a int,
) int { return a }
`, "type ID 3-3; type Repo 5-7; func Repo.Open 9-17; func helper 19-21"},
		{"lib.rs", `
pub struct Unit;

impl Repo {
    pub fn open(&self) -> Option<&'a str> {
        let c = '{';
        None
    }
}

pub trait T {
    fn f();
}

impl T for Unit {
    fn f() {}
}
`, "struct Unit 1-1; impl Repo 3-8; fn Repo.open 4-7; trait T 10-12; fn T.f 11-11; impl T for Unit 14-16; fn Unit.f 15-15"},
		{"m.py", `
import os

@decorator
def top(a,
        b):
    return a

class Repo:
    def open(self):
        if x:
            pass

    def close(self):
        pass

x = 1
`, "def top 4-6; class Repo 8-14; def Repo.open 9-11; def Repo.close 13-14"},
		{"a.ts", `
export function f(a: string): number {
  return a.length // }
}

export class C {
  m(x: number): void {
    console.log(` + "`${x}}`" + `)
  }
}
`, "function f 1-3; class C 5-9; method C.m 6-8"},
		{"a.rb", `
class Repo
  def open
    1
  end
end
`, "class Repo 1-5; def Repo.open 2-4"},
		{"README.md", `
# Tool

intro

## Install

text

## Usage

more
`, "h1 Tool 1-11; h2 Install 5-8; h2 Usage 9-11"},
		{"a.c", `
#define MAX(a, b) \
	((a) > (b) ? (a) : (b))

int helper(int x) {
	char c = '}';
	return x;
}
`, "define MAX 1-2; func helper 4-7"},
	}
	for _, c := range cases {
		if got := spans(c.name, c.src); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.want)
		}
	}
}

// "/*" inside a string must not hide everything after it.
func TestCommentMarkersInStrings(t *testing.T) {
	src := "package a\n\nfunc a() string { return \"/*\" }\n\nfunc b() {}\n\nvar u = \"http://x\"\n\nfunc c() {}\n"
	if got := spans("a.go", src); got != "func a 3-3; func b 5-5; func c 9-9" {
		t.Errorf("got %s", got)
	}
}

func TestGoMethodsCarryTheirType(t *testing.T) {
	src := "package a\n\nfunc (t *Tree[K]) Insert(k K) {}\n\nfunc (Tests) any() bool { return true }\n\nfunc plain() {}\n"
	if got := spans("a.go", src); got != "func Tree.Insert 3-3; func Tests.any 5-5; func plain 7-7" {
		t.Errorf("got %s", got)
	}
}

// Quotes inside JavaScript regex literals and single-quoted strings with
// braces must not throw off where functions end; a division is no regex.
func TestJavaScriptRegexAndQuotes(t *testing.T) {
	src := `export function escapeHtml(s) {
	return String(s)
		.replace(/"/g, "&quot;")
		.replace(/'/g, "&#39;");
}

function half(total, n) {
	const x = total / 2, y = n / 4;
	return x / y;
}

function tag() {
	return '<div class="{x}">' + '}';
}

function after() {}
`
	want := "function escapeHtml 1-5; function half 7-10; function tag 12-14; function after 16-16"
	if got := spans("a.js", src); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// an unclosed "…" ends with its line outside Rust
	if got := spans("a.c", "int a(void) {\n\tputs(\"x);\n}\n\nint b(void) {\n\treturn 1;\n}\n"); got != "func a 1-3; func b 5-7" {
		t.Errorf("got %s", got)
	}
}

// Helpers nested in a factory function and tab-indented class methods are
// definitions too.
func TestJavaScriptNestedDefinitions(t *testing.T) {
	src := "export function createMenu(runtime) {\n\tfunction bindPause() {\n\t\trun();\n\t}\n\tconst onKey = (e) => {\n\t\trun(e);\n\t};\n\treturn { bindPause };\n}\n\nclass View {\n\trender() {\n\t\treturn 1;\n\t}\n\tget size() {\n\t\treturn 2;\n\t}\n}\n"
	want := "function createMenu 1-9; function createMenu.bindPause 2-4; const createMenu.onKey 5-7; class View 11-18; method View.render 12-14; method View.size 15-17"
	if got := spans("a.js", src); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

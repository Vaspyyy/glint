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

// Braces in a parameter list (destructuring, an inline struct type) don't
// open the body.
func TestBracesInParameters(t *testing.T) {
	js := "export function build({\n\tmask,\n\tscratch = {},\n}) {\n\tconst a = 1;\n\treturn a;\n}\n\nfunction next() {}\n"
	if got := spans("a.js", js); got != "function build 1-7; function next 9-9" {
		t.Errorf("js: got %s", got)
	}
	g := "package a\n\nfunc f(x struct{ a int }) int {\n\treturn x.a\n}\n\nfunc g() {}\n"
	if got := spans("a.go", g); got != "func f 3-5; func g 7-7" {
		t.Errorf("go: got %s", got)
	}
}

// impl blocks with lifetimes or generics name their methods after the bare type.
func TestRustImplWithLifetimes(t *testing.T) {
	src := "impl<'a> Shape for Grid<'a> {\n    fn area(&self) -> f64 {\n        1.0\n    }\n}\n\nimpl<T: Copy> Stack<T> {\n    fn push(&mut self, x: T) {}\n}\n"
	want := "impl Shape for Grid<'a> 1-5; fn Grid.area 2-4; impl Stack<T> 7-9; fn Stack.push 8-8"
	if got := spans("a.rs", src); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Nested Python functions and classes are definitions too.
func TestPythonNested(t *testing.T) {
	src := "class Window:\n    def __init__(self):\n        def make():\n            return 1\n        self.x = make()\n\n    def show(self):\n        pass\n"
	want := "class Window 1-8; def Window.__init__ 2-5; def Window.__init__.make 3-4; def Window.show 7-8"
	if got := spans("a.py", src); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Functions assigned to properties and block-bodied arrow functions in
// object literals are definitions; a type annotation is not.
func TestJavaScriptPropertyFunctions(t *testing.T) {
	src := "function start() {\n\t_worker.onmessage = (evt) => {\n\t\thandle(evt);\n\t};\n\treturn {\n\t\tset: (v) => {\n\t\t\tx = v;\n\t\t},\n\t\tget: () => x,\n\t};\n}\n\ninterface Props {\n  onClick: (e: Event) => void;\n}\n"
	want := "function start 1-11; function start._worker.onmessage 2-4; method start.set 6-8; interface Props 13-15"
	if got := spans("a.ts", src); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := spans("a.py", "def über_limit():\n    return 1\n"); got != "def über_limit 1-2" {
		t.Errorf("py: got %s", got)
	}
}

// Rust impls nested in a module or function, tuple types in impl generics,
// and JavaScript's returned named functions are definitions.
func TestNestedImplsAndReturnedFunctions(t *testing.T) {
	rs := "mod tests {\n    struct Gradient;\n    impl DensitySampler for Gradient {\n        fn sample(&self) -> f64 {\n            1.0\n        }\n    }\n}\n\nimpl fmt::Display for Wrap<(&Res, Ctx<'_>)> {\n    fn fmt(&self) {}\n}\n"
	want := "mod tests 1-8; struct tests.Gradient 2-2; impl tests.DensitySampler for Gradient 3-7; fn tests.Gradient.sample 4-6; impl fmt::Display for Wrap<(&Res, Ctx<'_>)> 10-12; fn Wrap.fmt 11-11"
	if got := spans("a.rs", rs); got != want {
		t.Errorf("rust:\n got  %s\n want %s", got, want)
	}
	js := "function make(world) {\n\treturn function update(i) {\n\t\treturn i;\n\t};\n}\n"
	if got := spans("a.js", js); got != "function make 1-5; function make.update 2-4" {
		t.Errorf("js: got %s", got)
	}
}

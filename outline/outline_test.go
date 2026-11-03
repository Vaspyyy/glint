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
`, "type ID 3-3; type Repo 5-7; func Open 9-17; func helper 19-21"},
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
`, "struct Unit 1-1; impl Repo 3-8; fn Repo.open 4-7; trait T 10-12; fn T.f 11-11"},
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

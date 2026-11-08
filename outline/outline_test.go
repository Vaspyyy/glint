package outline

import "testing"

// The rules are tested in github.com/Vaspyyy/prism/outline; this only
// checks that the old import path still reaches them.
func TestForwards(t *testing.T) {
	src := []byte("package shop\n\ntype Repo struct{}\n\nfunc (r *Repo) Open(path string) error {\n\treturn nil\n}\n")
	if !Supported("repo.go") || Supported("repo.unknown") {
		t.Fatal("Supported doesn't forward")
	}
	syms := Parse("repo.go", src, 100)
	if len(syms) != 2 || Qualified(syms, 1) != "Repo.Open" || syms[1].Line != 5 || syms[1].End != 7 {
		t.Fatalf("Parse/Qualified don't forward: %+v", syms)
	}
	if MaxSize <= 0 {
		t.Fatal("MaxSize doesn't forward")
	}
}

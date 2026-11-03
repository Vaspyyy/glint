package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFreeName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.txt")
	if got := freeName(p); got != p {
		t.Fatalf("free path changed: %s", got)
	}
	os.WriteFile(p, nil, 0o644)
	if got := freeName(p); filepath.Base(got) != "notes (copy).txt" {
		t.Errorf("got %s", got)
	}
	os.WriteFile(filepath.Join(dir, "notes (copy).txt"), nil, 0o644)
	if got := freeName(p); filepath.Base(got) != "notes (copy 2).txt" {
		t.Errorf("got %s", got)
	}
	os.Mkdir(filepath.Join(dir, "v1.2"), 0o755)
	if got := freeName(filepath.Join(dir, "v1.2")); filepath.Base(got) != "v1.2 (copy)" {
		t.Errorf("folders keep their dots: got %s", got)
	}
}

func TestCopyTreeAndMove(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(src, "a", "b", "f.sh"), []byte("#!/bin/sh\n"), 0o755)
	os.Symlink("b/f.sh", filepath.Join(src, "a", "link"))

	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(filepath.Join(src, "a"), dst); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dst, "b", "f.sh"))
	if err != nil || st.Mode().Perm() != 0o755 {
		t.Errorf("file or mode not copied: %v %v", st, err)
	}
	if target, err := os.Readlink(filepath.Join(dst, "link")); err != nil || target != "b/f.sh" {
		t.Errorf("symlink not copied as a link: %q %v", target, err)
	}

	moved := filepath.Join(t.TempDir(), "moved")
	if err := move(filepath.Join(src, "a"), moved); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, "a")); !os.IsNotExist(err) {
		t.Error("source still exists after move")
	}
	if _, err := os.Stat(filepath.Join(moved, "b", "f.sh")); err != nil {
		t.Error("moved file missing")
	}
}

func TestPasteRefusesSelfNesting(t *testing.T) {
	dir := t.TempDir()
	m := &model{dir: filepath.Join(dir, "a", "b"), clip: &clipboard{paths: []string{filepath.Join(dir, "a")}},
		events: make(chan func(), 1)}
	m.paste()
	if m.msg != "⚠ can't paste a folder into itself" {
		t.Errorf("got %q", m.msg)
	}
}

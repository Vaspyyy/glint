//go:build unix

// Package pipetest is for tests of code that must not read a named pipe:
// it makes pipes, and runs a read with a time limit, because a reader
// that opens a pipe never comes back.
package pipetest

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Pipe makes a named pipe at path, and the folders above it.
func Pipe(t testing.TB, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Skip("no named pipes here:", err)
	}
}

// Both makes a named pipe and a symlink to it in dir, both called name
// with ext, and returns their paths.
func Both(t testing.TB, dir, ext string) []string {
	t.Helper()
	pipe, link := filepath.Join(dir, "pipe"+ext), filepath.Join(dir, "link"+ext)
	Pipe(t, pipe)
	if err := os.Symlink(filepath.Base(pipe), link); err != nil {
		t.Fatal(err)
	}
	return []string{pipe, link}
}

// Returns fails the test when f is still running after ten seconds.
func Returns(t testing.TB, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s is still waiting after 10s", what)
	}
}

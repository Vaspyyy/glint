package fsx

import (
	"slices"
	"testing"
)

func TestNaturalCompare(t *testing.T) {
	got := []string{"file10", "File2", ".config", "file1", "b", "file02x", "a", "Öffentlich", "Vorlagen", "Musik", "_proposal"}
	slices.SortFunc(got, NaturalCompare)
	want := []string{"a", "b", ".config", "file1", "File2", "file02x", "file10", "Musik", "Öffentlich", "_proposal", "Vorlagen"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

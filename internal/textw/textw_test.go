package textw

import "testing"

func TestWidth(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"hello", 5},
		{"\x1b[38;2;1;2;3;1mhi\x1b[0m", 2},
		{"\x1b]8;;file:///tmp/x\x1b\\x\x1b]8;;\x1b\\", 1},
		{"\ue627 main.go", 9},
		{"日本", 4},
		{"→ ok", 4},
	} {
		if got := Width(c.in); got != c.want {
			t.Errorf("Width(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("hello world", 6); got != "hello…" {
		t.Errorf("got %q", got)
	}
	if got := Width(Truncate("\x1b[31mhello world\x1b[0m", 6)); got != 6 {
		t.Errorf("styled width %d", got)
	}
	if got := Truncate("short", 10); got != "short" {
		t.Errorf("got %q", got)
	}
}

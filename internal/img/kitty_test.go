package img

import "testing"

func TestKittySupportedGoesByTheTerminal(t *testing.T) {
	for _, c := range []struct {
		term, program, window string
		want                  bool
	}{
		{"xterm-kitty", "", "", true},
		{"xterm-256color", "", "1", true},
		{"xterm-256color", "WezTerm", "", true},
		{"xterm-256color", "lumen", "", true},
		{"xterm-256color", "", "", false},
		{"xterm-256color", "vscode", "", false},
	} {
		t.Setenv("TERM", c.term)
		t.Setenv("TERM_PROGRAM", c.program)
		t.Setenv("KITTY_WINDOW_ID", c.window)
		if got := KittySupported(); got != c.want {
			t.Errorf("TERM=%q TERM_PROGRAM=%q KITTY_WINDOW_ID=%q: got %v, want %v", c.term, c.program, c.window, got, c.want)
		}
	}
}

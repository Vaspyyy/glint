package cli

import (
	"bytes"
	"testing"
)

// The tools agree on -V for the version and on how an unknown flag is told.
func TestVersionAndUnknownFlag(t *testing.T) {
	t.Setenv("GLINT_OPTS", "")
	for _, args := range [][]string{{"--version"}, {"-V"}, {"-v"}} {
		var out, errOut bytes.Buffer
		if status := run(append(args, "--no-config"), &out, &errOut); status != 0 || out.String() != "glint "+version+"\n" {
			t.Errorf("glint %q: status %d, out %q, err %q", args, status, out.String(), errOut.String())
		}
	}
	for args, want := range map[string]string{
		"--bogus": "glint: unknown flag --bogus (see glint --help)\n",
		"-Q":      "glint: unknown flag -Q (see glint --help)\n",
	} {
		var out, errOut bytes.Buffer
		if status := run([]string{"--no-config", args}, &out, &errOut); status != 2 || errOut.String() != want {
			t.Errorf("glint %s: status %d, err %q, want %q", args, status, errOut.String(), want)
		}
	}
}

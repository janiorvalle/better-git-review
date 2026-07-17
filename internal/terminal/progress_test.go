package terminal

import (
	"bytes"
	"strings"
	"testing"
)

func TestProgressGatingPreservesPlainBytes(t *testing.T) {
	for _, test := range []struct {
		name    string
		tty     bool
		noColor bool
	}{
		{name: "non tty"},
		{name: "no color", tty: true, noColor: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			progress := New(&output, test.tty, test.noColor)
			progress.Logf("parsed %d changed file(s)", 2)
			progress.Provider("mock", "deterministic", "low")
			spinner := progress.Start("analyzing...")
			spinner.Stop()
			progress.Wrote("out.html", false)
			want := "  parsed 2 changed file(s)\n  provider: \"mock\" / model: \"deterministic\" / reasoning: \"low\"\n\n  wrote out.html\n"
			if output.String() != want || strings.Contains(output.String(), "\x1b") {
				t.Fatalf("output = %q, want %q", output.String(), want)
			}
		})
	}
}

func TestTTYProgressUsesANSI(t *testing.T) {
	var output bytes.Buffer
	progress := New(&output, true, false)
	progress.Logf("source")
	spinner := progress.Start("analyzing...")
	spinner.Stop()
	if !strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("styled output has no ANSI: %q", output.String())
	}
}

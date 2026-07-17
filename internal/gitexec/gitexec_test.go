package gitexec

import (
	"slices"
	"testing"
)

func TestGitArgumentsCarrySharedHardening(t *testing.T) {
	diff := Harden(DiffArgs("main...HEAD")...)
	blame := Harden(BlameArgs("topic", "3,8", "main.go")...)
	for _, args := range [][]string{diff, blame} {
		if !slices.Contains(args, "color.ui=false") {
			t.Fatalf("shared color hardening missing: %#v", args)
		}
	}
	for _, expected := range []string{"diff.mnemonicPrefix=false", "--no-ext-diff", "--no-textconv", "--no-color"} {
		if !slices.Contains(diff, expected) {
			t.Fatalf("diff hardening missing %q: %#v", expected, diff)
		}
	}
	for _, expected := range []string{"--porcelain", "--no-textconv"} {
		if !slices.Contains(blame, expected) {
			t.Fatalf("blame hardening missing %q: %#v", expected, blame)
		}
	}
}

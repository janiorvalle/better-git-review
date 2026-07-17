package agentskill

import (
	"bytes"
	"os"
	"testing"
)

func TestEmbeddedSkillInstallsIdempotently(t *testing.T) {
	if !bytes.Contains(Content, []byte("--format json")) {
		t.Fatal("embedded skill is missing its machine-mode guidance")
	}
	home := t.TempDir()
	for _, target := range []Target{Claude, Codex} {
		first, err := Install(home, target)
		if err != nil {
			t.Fatal(err)
		}
		second, err := Install(home, target)
		if err != nil || first != second {
			t.Fatalf("idempotent install failed: %q %q %v", first, second, err)
		}
		data, err := os.ReadFile(second)
		if err != nil || !bytes.Equal(data, Content) {
			t.Fatalf("installed content mismatch: %v", err)
		}
	}
}
